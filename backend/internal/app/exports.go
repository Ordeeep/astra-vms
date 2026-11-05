package app

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/media"
)

func (a *App) exportAuthorized(ctx context.Context, actor, tenant string, plan media.ExportPlan) bool {
	return exportAuthorizedWith(ctx, a.db, actor, tenant, plan)
}
func exportAuthorizedWith(ctx context.Context, db permissionReader, actor, tenant string, plan media.ExportPlan) bool {
	var active bool
	if db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operators o JOIN memberships m ON m.operator_id=o.id JOIN tenants t ON t.id=m.tenant_id WHERE o.id=$1 AND t.id=$2 AND o.active AND t.active)", actor, tenant).Scan(&active) != nil || !active {
		return false
	}
	grants, err := queryGrants(ctx, db, actor, tenant)
	if err != nil || plan.Tenant != tenant || len(plan.Clips) == 0 {
		return false
	}
	for _, clip := range plan.Clips {
		if !permits(grants, tenant, "playback", clip.Index.Site, clip.Index.Camera) || !permits(grants, tenant, "export", clip.Index.Site, clip.Index.Camera) {
			return false
		}
	}
	return true
}
func buildExportPlan(tenant, camera string, start, end time.Time, segments []archiveSegment) (media.ExportPlan, error) {
	plan := media.ExportPlan{Tenant: tenant, Camera: camera, Start: start, End: end, FPS: 1, Gaps: []media.ExportGap{}}
	last := start
	for _, s := range segments {
		index := s.Index
		if index.Start.Before(last.Add(-time.Microsecond)) && !last.Equal(start) {
			return plan, errors.New("overlapping archive epochs")
		}
		if index.Start.After(last) {
			plan.Gaps = append(plan.Gaps, media.ExportGap{Start: last, End: minTime(index.Start, end)})
		}
		for _, part := range index.Parts {
			plan.Width = max(plan.Width, part.Width)
			plan.Height = max(plan.Height, part.Height)
			durations := part.Durations
			if len(part.PresentationDurations) == part.Frames && part.Frames > 0 {
				durations = part.PresentationDurations
			}
			for _, ticks := range durations {
				if ticks == 0 {
					return plan, errors.New("invalid frame duration")
				}
				plan.FPS = max(plan.FPS, int(math.Ceil(90000/float64(ticks))))
			}
		}
		plan.Clips = append(plan.Clips, media.ExportClip{ID: s.ID, Index: index, MetadataHash: media.IndexHash(index)})
		last = maxTime(last, minTime(index.End, end))
	}
	if last.Before(end) {
		plan.Gaps = append(plan.Gaps, media.ExportGap{Start: last, End: end})
	}
	plan.Width = (plan.Width + 1) / 2 * 2
	plan.Height = (plan.Height + 1) / 2 * 2
	if err := plan.Validate(); err != nil {
		return plan, err
	}
	// The worker owns the immutable frame index. Carry its digest and origin,
	// not millions of per-frame durations, across the control API.
	for i := range plan.Clips {
		plan.Clips[i].Index.Parts = nil
	}
	return plan, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func (a *App) exportPreview(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Camera   string    `json:"cameraId"`
		Start    time.Time `json:"start"`
		End      time.Time `json:"end"`
		Timezone string    `json:"timezone"`
	}
	if !body(w, r, &in) {
		return
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		problem(w, 422, "invalid_timezone")
		return
	}
	if !in.End.After(in.Start) || in.End.Sub(in.Start) > 24*time.Hour {
		problem(w, 422, "invalid_export_window")
		return
	}
	segments, err := a.segments(r.Context(), tenant, in.Camera, in.Start, in.End)
	if err != nil {
		dbFailure(w)
		return
	}
	if len(segments) == 0 {
		problem(w, 422, "export_has_no_images")
		return
	}
	plan, err := buildExportPlan(tenant, in.Camera, in.Start, in.End, segments)
	if err != nil {
		problem(w, 422, "archive_requires_review")
		return
	}
	if !a.exportAuthorized(r.Context(), id.User.ID, tenant, plan) {
		problem(w, 403, "forbidden")
		return
	}
	plan.Requestor = id.User.ID
	plan.DisplayTimezone = in.Timezone
	preview := newID()
	raw, _ := json.Marshal(plan)
	estimate := max(int64(1<<20), int64(in.End.Sub(in.Start).Seconds()*float64(plan.Width*plan.Height*plan.FPS)*.015)+int64(len(segments))*1024)
	if _, err = a.db.Exec(r.Context(), "INSERT INTO export_previews(id,tenant_id,actor_id,plan,estimated_bytes,expires) VALUES($1,$2,$3,$4,$5,now()+interval '30 minutes')", preview, tenant, id.User.ID, raw, estimate); err != nil {
		dbFailure(w)
		return
	}
	response(w, 201, map[string]any{"id": preview, "revision": 1, "start": in.Start, "end": in.End, "gaps": plan.Gaps, "fps": plan.FPS, "width": plan.Width, "height": plan.Height, "estimatedBytes": estimate, "frames": plan.Frames(), "normalization": "H.264; quadros em taxa constante; preto nas lacunas; sem áudio ou marca", "expiresInSeconds": 1800})
}

type exportJob struct {
	ID      string             `json:"id"`
	Tenant  string             `json:"-"`
	Actor   string             `json:"requestedBy"`
	Status  string             `json:"status"`
	Code    string             `json:"code"`
	Created time.Time          `json:"createdAt"`
	Expires time.Time          `json:"expires"`
	Result  media.ExportStatus `json:"result"`
	Plan    media.ExportPlan   `json:"-"`
	Preview string             `json:"-"`
}

const exportSelect = "SELECT id,tenant_id,actor_id,status,code,created_at,expires,result,plan,preview_id FROM export_jobs"

func scanExport(row interface{ Scan(...any) error }) (exportJob, error) {
	var job exportJob
	var result, plan []byte
	err := row.Scan(&job.ID, &job.Tenant, &job.Actor, &job.Status, &job.Code, &job.Created, &job.Expires, &result, &plan, &job.Preview)
	if err == nil {
		err = json.Unmarshal(plan, &job.Plan)
	}
	if err == nil {
		err = json.Unmarshal(result, &job.Result)
	}
	return job, err
}
func (a *App) createExport(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Preview  string `json:"previewId"`
		Revision int    `json:"revision"`
		Key      string `json:"idempotencyKey"`
	}
	if !body(w, r, &in) {
		return
	}
	if in.Revision != 1 || len(in.Key) < 16 || len(in.Key) > 128 {
		problem(w, 422, "invalid_idempotency")
		return
	}
	tx, id, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	existing, err := scanExport(tx.QueryRow(r.Context(), exportSelect+" WHERE tenant_id=$1 AND actor_id=$2 AND idempotency_key=$3", tenant, id.User.ID, in.Key))
	if err == nil {
		if existing.Preview != in.Preview {
			problem(w, 409, "idempotency_conflict")
			return
		}
		if !exportAuthorizedWith(r.Context(), tx, id.User.ID, tenant, existing.Plan) {
			problem(w, 403, "forbidden")
			return
		}
		response(w, 202, existing)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		dbFailure(w)
		return
	}
	var raw []byte
	var estimate int64
	if tx.QueryRow(r.Context(), "SELECT plan,estimated_bytes FROM export_previews WHERE id=$1 AND tenant_id=$2 AND actor_id=$3 AND expires>now()", in.Preview, tenant, id.User.ID).Scan(&raw, &estimate) != nil {
		problem(w, 409, "export_preview_expired")
		return
	}
	var plan media.ExportPlan
	if json.Unmarshal(raw, &plan) != nil || !exportAuthorizedWith(r.Context(), tx, id.User.ID, tenant, plan) {
		problem(w, 403, "forbidden")
		return
	}
	if !exportSourcesAvailableWith(r.Context(), tx, plan) {
		problem(w, 409, "source_changed_repreview")
		return
	}
	var budget int64
	var hours int
	if tx.QueryRow(r.Context(), "SELECT export_bytes,export_hours FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&budget, &hours) != nil {
		problem(w, 409, "storage_policy_required")
		return
	}
	var usage map[string]int64
	if a.worker(r.Context(), "GET", "/archive/usage?tenant="+url.QueryEscape(tenant), nil, &usage) != nil {
		problem(w, 503, "storage_unavailable")
		return
	}
	if estimate > budget-usage["export"] {
		problem(w, 409, "export_budget_unavailable")
		return
	}
	var queued int
	if tx.QueryRow(r.Context(), "SELECT count(*) FROM export_jobs WHERE status IN ('queued','running')").Scan(&queued) != nil {
		dbFailure(w)
		return
	}
	if queued >= 100 {
		problem(w, 429, "export_queue_full")
		return
	}
	job := exportJob{ID: newID(), Tenant: tenant, Actor: id.User.ID, Status: "queued", Created: time.Now().UTC(), Expires: time.Now().UTC().Add(time.Duration(hours) * time.Hour), Plan: plan, Preview: in.Preview}
	job.Plan.ID = job.ID
	job.Plan.CreatedAt = job.Created
	raw, _ = json.Marshal(job.Plan)
	if _, err = tx.Exec(r.Context(), "INSERT INTO export_jobs(id,tenant_id,actor_id,preview_id,idempotency_key,plan,status,expires) VALUES($1,$2,$3,$4,$5,$6,'queued',$7)", job.ID, tenant, id.User.ID, in.Preview, in.Key, raw, job.Expires); err != nil {
		dbFailure(w)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) VALUES($1,$2,'export.requested',$3)", id.User.ID, tenant, job.ID); err != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	response(w, 202, job)
}
func (a *App) exportSourcesAvailable(ctx context.Context, plan media.ExportPlan) bool {
	return exportSourcesAvailableWith(ctx, a.db, plan)
}
func exportSourcesAvailableWith(ctx context.Context, db permissionReader, plan media.ExportPlan) bool {
	ids := make([]string, 0, len(plan.Clips))
	expected := map[string]string{}
	for _, clip := range plan.Clips {
		ids = append(ids, clip.ID)
		expected[clip.ID] = clip.MetadataHash
	}
	rows, err := db.Query(ctx, "SELECT id,metadata FROM archive_segments WHERE tenant_id=$1 AND id=ANY($2) AND available AND EXISTS(SELECT 1 FROM cameras WHERE cameras.tenant_id=archive_segments.tenant_id AND cameras.id=archive_segments.camera_id AND cameras.deleted_at IS NULL)", plan.Tenant, ids)
	if err != nil {
		return false
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			return false
		}
		var index media.SegmentIndex
		if json.Unmarshal(raw, &index) != nil || media.IndexHash(index) != expected[id] {
			return false
		}
		count++
	}
	return rows.Err() == nil && count == len(expected) && count > 0
}
func (a *App) exports(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), exportSelect+" WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 201", tenant, r.URL.Query().Get("cursor"))
	if err != nil {
		dbFailure(w)
		return
	}
	all := []exportJob{}
	for rows.Next() {
		job, e := scanExport(rows)
		if e != nil {
			rows.Close()
			dbFailure(w)
			return
		}
		all = append(all, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		dbFailure(w)
		return
	}
	next := ""
	if len(all) > 200 {
		next = all[199].ID
		all = all[:200]
	}
	items := []exportJob{}
	for _, job := range all {
		if a.exportAuthorized(r.Context(), id.User.ID, tenant, job.Plan) {
			items = append(items, job)
		}
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": next})
}
func (a *App) exportResource(w http.ResponseWriter, r *http.Request, id identity) {
	if r.Header.Get("X-VMS-Context") == "" {
		r.Header.Set("X-VMS-Context", r.URL.Query().Get("context"))
	}
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	job, err := scanExport(a.db.QueryRow(r.Context(), exportSelect+" WHERE id=$1 AND tenant_id=$2", r.PathValue("export"), tenant))
	if err != nil {
		problem(w, 404, "export_not_found")
		return
	}
	if !a.exportAuthorized(r.Context(), id.User.ID, tenant, job.Plan) {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "POST" {
		if job.Actor != id.User.ID && !a.allowed(r.Context(), id, tenant, "recording.manage", "", "") {
			problem(w, 403, "forbidden")
			return
		}
		changed, e := a.db.Exec(r.Context(), "UPDATE export_jobs SET status='cancelled',code='cancelled_by_operator' WHERE id=$1 AND status IN ('queued','running')", job.ID)
		err = e
		if err != nil {
			dbFailure(w)
			return
		}
		if changed.RowsAffected() == 0 {
			problem(w, 409, "export_finished")
			return
		}
		a.worker(r.Context(), "DELETE", "/archive/exports/"+job.ID+"?tenant="+url.QueryEscape(tenant), nil, nil)
		a.db.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) VALUES($1,$2,'export.cancelled',$3)", id.User.ID, tenant, job.ID)
		w.WriteHeader(204)
		return
	}
	artifact := r.PathValue("artifact")
	if artifact == "" {
		response(w, 200, job)
		return
	}
	if job.Status != "completed" || !time.Now().Before(job.Expires) {
		problem(w, 410, "export_unavailable")
		return
	}
	object, size := job.Result.Content, job.Result.Bytes
	if artifact == "report" {
		object, size = job.Result.Report, job.Result.ReportBytes
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+job.ID+`.json"`)
	} else if artifact == "content" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+job.ID+`.mp4"`)
	} else {
		problem(w, 404, "export_not_found")
		return
	}
	authorized := func(ctx context.Context) bool {
		var valid bool
		return a.db.QueryRow(ctx, "SELECT status='completed' AND expires>now() FROM export_jobs WHERE id=$1", job.ID).Scan(&valid) == nil && valid && a.sessionStillActive(ctx, id) && contextValid(ctx, a.db, r, id) && a.exportAuthorized(ctx, id.User.ID, tenant, job.Plan)
	}
	a.proxyArchiveBytes(w, r, tenant, object, 0, size, authorized)
}
func (a *App) exportLoop() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	cycles := 0
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
		cycles++
		a.recordingMu.Lock()
		if a.quotaDirty {
			a.recordingMu.Unlock()
			continue
		}
		ctx, cancel := context.WithTimeout(a.runContext, 1200*time.Millisecond)
		deadline := time.Now().Add(1400 * time.Millisecond)
		job, err := scanExport(a.db.QueryRow(ctx, exportSelect+" WHERE status IN ('queued','running') ORDER BY created_at,id LIMIT 1"))
		if err == nil {
			path := "/archive/exports/" + job.ID + "?tenant=" + url.QueryEscape(job.Tenant)
			if !a.exportAuthorized(ctx, job.Actor, job.Tenant, job.Plan) || !time.Now().Before(job.Expires) {
				a.db.Exec(ctx, "UPDATE export_jobs SET status='failed',code='requestor_access_revoked_or_expired' WHERE id=$1 AND status IN ('queued','running')", job.ID)
				a.worker(ctx, "DELETE", path, nil, nil)
			} else if job.Status == "queued" {
				if !a.exportSourcesAvailable(ctx, job.Plan) {
					a.db.Exec(ctx, "UPDATE export_jobs SET status='failed',code='source_changed_repreview' WHERE id=$1 AND status='queued'", job.ID)
				} else {
					command, e := a.db.Exec(ctx, "UPDATE export_jobs SET status='running' WHERE id=$1 AND status='queued'", job.ID)
					if e == nil && command.RowsAffected() == 1 {
						var status media.ExportStatus
						e = a.worker(ctx, "POST", "/archive/exports", media.ExportRequest{Plan: job.Plan, Expires: deadline, ArtifactExpires: job.Expires}, &status)
						var rejected workerError
						if errors.As(e, &rejected) {
							if rejected.Status == 409 || rejected.Status == 429 {
								a.db.Exec(ctx, "UPDATE export_jobs SET status='queued' WHERE id=$1 AND status='running'", job.ID)
							} else if rejected.Status == 422 || rejected.Status == 413 {
								a.db.Exec(ctx, "UPDATE export_jobs SET status='failed',code='export_plan_rejected' WHERE id=$1 AND status='running'", job.ID)
							}
						}
					}
				}
			} else {
				var status media.ExportStatus
				e := a.worker(ctx, "GET", path, nil, &status)
				if e == nil {
					if status.Status == "running" {
						a.worker(ctx, "PATCH", path, map[string]time.Time{"expires": deadline}, nil)
						raw, _ := json.Marshal(status)
						a.db.Exec(ctx, "UPDATE export_jobs SET result=$2 WHERE id=$1 AND status='running'", job.ID, raw)
					} else {
						a.finishExport(ctx, job, status)
					}
				} else {
					var rejected workerError
					if errors.As(e, &rejected) && rejected.Status == 404 {
						a.db.Exec(ctx, "UPDATE export_jobs SET status='failed',code='worker_job_lost' WHERE id=$1 AND status='running'", job.ID)
					}
				}
			}
		}
		cancel()
		a.recordingMu.Unlock()
		if cycles%25 == 0 {
			a.cleanExports()
		}
	}
}
func (a *App) finishExport(ctx context.Context, job exportJob, status media.ExportStatus) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(78921452)"); err != nil {
		return
	}
	if !exportAuthorizedWith(ctx, tx, job.Actor, job.Tenant, job.Plan) {
		status.Status = "failed"
		status.Code = "requestor_access_revoked_or_expired"
	}
	raw, _ := json.Marshal(status)
	changed, err := tx.Exec(ctx, "UPDATE export_jobs SET status=$2,code=$3,result=$4 WHERE id=$1 AND status='running'", job.ID, status.Status, status.Code, raw)
	if err != nil {
		return
	}
	if changed.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,details) VALUES($1,$2,'export.finished',$3,$4)", job.Actor, job.Tenant, job.ID, raw); err != nil {
			return
		}
	}
	tx.Commit(ctx)
}
func (a *App) cleanExports() {
	ctx, cancel := context.WithTimeout(a.runContext, 2*time.Second)
	defer cancel()
	a.db.Exec(ctx, "DELETE FROM export_previews WHERE expires<now()")
	rows, err := a.db.Query(ctx, exportSelect+" WHERE (expires<now() OR status IN ('failed','cancelled')) AND NOT artifacts_cleaned ORDER BY created_at LIMIT 100")
	if err != nil {
		return
	}
	jobs := []exportJob{}
	for rows.Next() {
		job, e := scanExport(rows)
		if e == nil {
			jobs = append(jobs, job)
		}
	}
	rows.Close()
	for _, job := range jobs {
		ok := a.worker(ctx, "DELETE", "/archive/exports/"+job.ID+"?tenant="+url.QueryEscape(job.Tenant), nil, nil) == nil
		for _, object := range []string{job.Result.Content, job.Result.Report} {
			if object != "" && a.worker(ctx, "DELETE", "/archive/objects/"+object+"?tenant="+url.QueryEscape(job.Tenant), nil, nil) != nil {
				ok = false
			}
		}
		if ok {
			a.db.Exec(ctx, "UPDATE export_jobs SET artifacts_cleaned=true,status=CASE WHEN expires<now() THEN 'expired' ELSE status END WHERE id=$1", job.ID)
		}
	}
}
func (a *App) registerExports(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/export-previews", a.protected(a.exportPreview))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/exports", a.protected(a.createExport))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/exports", a.protected(a.exports))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/exports/{export}", a.protected(a.exportResource))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/exports/{export}/{artifact}", a.protected(a.exportResource))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/exports/{export}/cancel", a.protected(a.exportResource))
}
