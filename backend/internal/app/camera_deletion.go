package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/media"
)

// Keep the camera identity for provenance and completed exports. The tombstone,
// revocation and durable cleanup request commit together; no disk I/O precedes it.
func (a *App) deleteCamera(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !body(w, r, &in) {
		return
	}
	if in.Revision < 1 {
		problem(w, 422, "invalid_camera")
		return
	}
	a.recordingMu.Lock()
	defer a.recordingMu.Unlock()
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	cid := r.PathValue("camera")
	var site, name string
	var revision int64
	var deleted *time.Time
	err = tx.QueryRow(r.Context(), "SELECT site_id,name,revision,deleted_at FROM cameras WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, cid).Scan(&site, &name, &revision, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "camera_not_found")
		return
	}
	if err != nil {
		dbFailure(w)
		return
	}
	if !allowedWith(r.Context(), tx, current, tenant, "camera.delete", site, cid) {
		problem(w, 403, "forbidden")
		return
	}
	if deleted != nil {
		response(w, 202, map[string]string{"code": "camera_deleted", "message": "Câmera excluída. As gravações serão removidas em segundo plano."})
		return
	}
	if revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{"UPDATE cameras SET active=false,deleted_at=now(),revision=revision+1 WHERE tenant_id=$1 AND id=$2", []any{tenant, cid}},
		{"INSERT INTO camera_deletions(tenant_id,camera_id,actor_id) VALUES($1,$2,$3)", []any{tenant, cid, current.User.ID}},
		{"UPDATE camera_sources SET secrets=''::bytea,connection='null',profiles='[]' WHERE tenant_id=$1 AND camera_id=$2", []any{tenant, cid}},
		{"DELETE FROM recording_configs WHERE tenant_id=$1 AND camera_id=$2", []any{tenant, cid}},
		{"DELETE FROM playback_sessions WHERE tenant_id=$1 AND camera_id=$2", []any{tenant, cid}},
		{"UPDATE live_sessions SET expires=now() WHERE tenant_id=$1 AND camera_id=$2", []any{tenant, cid}},
		{"UPDATE export_jobs SET status='cancelled',code='camera_deleted' WHERE tenant_id=$1 AND plan->>'camera'=$2 AND status IN ('queued','running')", []any{tenant, cid}},
		{"DELETE FROM export_previews WHERE tenant_id=$1 AND plan->>'camera'=$2", []any{tenant, cid}},
		// Empty the slot instead of removing it, preserving the saved mosaic layout.
		{`UPDATE mosaics SET positions=(SELECT jsonb_agg(CASE WHEN p->>'cameraId'=$2 THEN '{"cameraId":"","profile":""}'::jsonb ELSE p END ORDER BY n) FROM jsonb_array_elements(positions) WITH ORDINALITY AS x(p,n)),revision=revision+1 WHERE tenant_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(positions) p WHERE p->>'cameraId'=$2)`, []any{tenant, cid}},
	}
	for _, statement := range statements {
		if _, err = tx.Exec(r.Context(), statement.sql, statement.args...); err != nil {
			dbFailure(w)
			return
		}
	}
	if audit(r.Context(), tx, current, tenant, "camera.deleted", cid, map[string]any{"name": name, "siteId": site, "recordings": "pending"}) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	a.invalidateAvailability(tenant, cid, "")
	response(w, 202, map[string]string{"code": "camera_deleted", "message": "Câmera excluída. As gravações serão removidas em segundo plano."})
}

func (a *App) cameraDeletionLoop() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
		a.cleanDeletedCamera()
	}
}

func (a *App) cleanDeletedCamera() {
	ctx, cancel := context.WithTimeout(a.runContext, 3*time.Second)
	defer cancel()
	var tenant, camera, actor, cursor, previous string
	if a.db.QueryRow(ctx, `SELECT tenant_id,camera_id,actor_id,cursor,last_error FROM camera_deletions WHERE status='pending' AND next_attempt<=now() ORDER BY next_attempt,created_at LIMIT 1`).Scan(&tenant, &camera, &actor, &cursor, &previous) != nil {
		return
	}
	// No capture or export command can be racing the final stop. Deletion already
	// removed the SQL configuration, so subsequent planner cycles cannot restart it.
	a.recordingMu.Lock()
	err := a.stopDeletedCamera(ctx, tenant, camera)
	a.recordingMu.Unlock()
	var page media.CameraCleanupPage
	if err == nil {
		err = a.worker(ctx, "POST", "/archive/camera-cleanup", media.CameraCleanupRequest{Tenant: tenant, Camera: camera, Cursor: cursor}, &page)
	}
	// Persist retry/failure information even if the worker consumed its deadline.
	save, done := context.WithTimeout(a.runContext, time.Second)
	defer done()
	tx, e := a.db.Begin(save)
	if e != nil {
		return
	}
	defer tx.Rollback(save)
	if err != nil {
		code := "camera_cleanup_unavailable"
		var rejected workerError
		if errors.As(err, &rejected) && rejected.Status == 409 {
			code = "camera_files_in_use"
		}
		if _, e = tx.Exec(save, `UPDATE camera_deletions SET attempts=attempts+1,last_error=$3,next_attempt=now()+make_interval(secs=>LEAST(60,2+attempts*2)) WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera, code); e != nil {
			return
		}
		// Repeated identical retries remain in the durable job, avoiding audit spam.
		if previous != code {
			_, e = tx.Exec(save, `INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,outcome,details) VALUES($1,$2,'camera.cleanup_pending',$3,'failed',jsonb_build_object('code',$4::text))`, actor, tenant, camera, code)
		}
	} else if page.Done {
		// The worker enumerated its complete catalogue after capture finalization,
		// including objects not yet imported by archiveMaintenance.
		_, e = tx.Exec(save, `UPDATE archive_segments SET available=false,discard_reason='camera_deleted' WHERE tenant_id=$1 AND camera_id=$2 AND available`, tenant, camera)
		if e == nil {
			_, e = tx.Exec(save, `UPDATE camera_deletions SET status='completed',completed_at=now(),last_error='',attempts=attempts+1 WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera)
		}
		if e == nil {
			_, e = tx.Exec(save, `INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) VALUES($1,$2,'camera.cleanup_completed',$3)`, actor, tenant, camera)
		}
	} else {
		_, e = tx.Exec(save, `UPDATE camera_deletions SET cursor=$3,last_error='',attempts=attempts+1,next_attempt=now() WHERE tenant_id=$1 AND camera_id=$2`, tenant, camera, page.NextCursor)
	}
	if e == nil {
		tx.Commit(save)
	}
}

func (a *App) stopDeletedCamera(ctx context.Context, tenant, camera string) error {
	if err := a.worker(ctx, "DELETE", "/captures?tenant="+url.QueryEscape(tenant)+"&camera="+url.QueryEscape(camera), nil, nil); err != nil {
		return err
	}
	rows, err := a.db.Query(ctx, "SELECT id,worker_token FROM live_sessions WHERE tenant_id=$1 AND camera_id=$2 LIMIT 100", tenant, camera)
	if err != nil {
		return err
	}
	type session struct{ id, token string }
	sessions := []session{}
	for rows.Next() {
		var s session
		if err = rows.Scan(&s.id, &s.token); err != nil {
			break
		}
		sessions = append(sessions, s)
	}
	scanErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	for _, s := range sessions {
		if err = a.worker(ctx, "DELETE", "/leases/"+url.PathEscape(s.token), nil, nil); err != nil {
			return err
		}
		if _, err = a.db.Exec(ctx, "DELETE FROM live_sessions WHERE id=$1 AND tenant_id=$2", s.id, tenant); err != nil {
			return err
		}
	}
	// Cancellation is idempotent and awaits worker readers before archive cleanup.
	rows, err = a.db.Query(ctx, `SELECT id FROM export_jobs WHERE tenant_id=$1 AND plan->>'camera'=$2 AND code='camera_deleted' AND NOT artifacts_cleaned LIMIT 100`, tenant, camera)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	scanErr = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	for _, id := range ids {
		if err = a.worker(ctx, "DELETE", "/archive/exports/"+url.PathEscape(id)+"?tenant="+url.QueryEscape(tenant), nil, nil); err != nil {
			return err
		}
		if _, err = a.db.Exec(ctx, "UPDATE export_jobs SET artifacts_cleaned=true WHERE id=$1 AND tenant_id=$2", id, tenant); err != nil {
			return err
		}
	}
	if len(sessions) == 100 || len(ids) == 100 {
		return errors.New("camera consumers still pending")
	}
	return nil
}
