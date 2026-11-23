package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"vms.local/backend/internal/media"
)

type exportTaskSummary struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"createdAt"`
	CameraName      string    `json:"cameraName"`
	Start           string    `json:"start"`
	End             string    `json:"end"`
	Author          string    `json:"author"`
	Status          string    `json:"status"`
	Code            string    `json:"code,omitempty"`
	Frames          int64     `json:"frames"`
	Total           int64     `json:"total"`
	HasGaps         bool      `json:"hasGaps"`
	CanDownload     bool      `json:"canDownload"`
	DownloadExpired bool      `json:"downloadExpired"`
}

func exportTaskOutcome(status, code, workerStatus string) string {
	if status != "expired" {
		return status
	}
	// Older cleanup versions replaced the outcome with 'expired'.
	switch {
	case code == "cancelled_by_operator" || code == "camera_deleted" || workerStatus == "cancelled":
		return "cancelled"
	case code != "" || workerStatus == "failed":
		return "failed"
	case workerStatus == "completed":
		return "completed"
	default:
		return "expired"
	}
}

func (a *App) exportTasks(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	grants, err := a.grants(ctx, id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	const pageSize = 30
	rows, err := a.db.Query(ctx, `SELECT j.id,j.created_at,
COALESCE(NULLIF(j.camera_name,''),c.name,j.plan->>'camera',''),
COALESCE(j.plan->>'start',''),COALESCE(j.plan->>'end',''),
COALESCE(NULLIF(j.author_name,''),NULLIF(o.name,''),o.username),
j.status,j.code,COALESCE(j.result->>'status',''),
COALESCE((j.result->>'frames')::bigint,0),COALESCE((j.result->>'total')::bigint,0),
CASE WHEN jsonb_typeof(j.plan->'gaps')='array' THEN jsonb_array_length(j.plan->'gaps')>0 ELSE false END,
j.expires<=now(),
CASE WHEN j.status='completed' AND j.expires>now() AND NOT j.artifacts_cleaned AND j.result->>'content'<>'' THEN j.plan END
FROM export_jobs j JOIN operators o ON o.id=j.actor_id
LEFT JOIN cameras c ON c.tenant_id=j.tenant_id AND c.id=j.plan->>'camera'
WHERE j.tenant_id=$1 AND j.actor_id=$2
AND ($3='' OR (j.created_at,j.id)<(SELECT created_at,id FROM export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND id=$3))
ORDER BY j.created_at DESC,j.id DESC LIMIT $4`, tenant, id.User.ID, r.URL.Query().Get("cursor"), pageSize+1)
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []exportTaskSummary{}
	for rows.Next() {
		var task exportTaskSummary
		var workerStatus string
		var expired bool
		var rawPlan []byte
		if err := rows.Scan(&task.ID, &task.CreatedAt, &task.CameraName, &task.Start, &task.End, &task.Author,
			&task.Status, &task.Code, &workerStatus, &task.Frames, &task.Total, &task.HasGaps, &expired, &rawPlan); err != nil {
			dbFailure(w)
			return
		}
		task.Status = exportTaskOutcome(task.Status, task.Code, workerStatus)
		task.DownloadExpired = task.Status == "completed" && expired
		if len(rawPlan) > 0 {
			var plan media.ExportPlan
			task.CanDownload = json.Unmarshal(rawPlan, &plan) == nil && exportPermitted(grants, tenant, plan)
		}
		items = append(items, task)
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	var active bool
	if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND status IN ('queued','running'))`, tenant, id.User.ID).Scan(&active); err != nil {
		dbFailure(w)
		return
	}
	var next string
	if len(items) > pageSize {
		items = items[:pageSize]
		next = items[pageSize-1].ID
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": next, "hasActive": active})
}

func (a *App) cancelExports(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	job := r.PathValue("export")
	count, running, exists, err := a.markExportsCancelled(r, id, tenant, job)
	if err != nil {
		dbFailure(w)
		return
	}
	if job != "" && count == 0 {
		if exists {
			problem(w, 409, "export_finished")
		} else {
			problem(w, 404, "export_not_found")
		}
		return
	}
	if len(running) > 0 {
		// The durable cancellation also stops lease renewal and triggers cleanup
		// if the worker cannot acknowledge this immediate stop.
		ctx, done := context.WithTimeout(a.runContext, 2*time.Second)
		defer done()
		for _, job := range running {
			a.worker(ctx, "DELETE", "/archive/exports/"+job+"?tenant="+url.QueryEscape(tenant), nil, nil)
		}
	}
	if job != "" {
		w.WriteHeader(204)
		return
	}
	response(w, 200, map[string]int{"cancelled": count})
}

func (a *App) markExportsCancelled(r *http.Request, id identity, tenant, job string) (count int, running []string, exists bool, err error) {
	// Serialize with dispatch so the worker cannot start a job after its stop.
	a.recordingMu.Lock()
	defer a.recordingMu.Unlock()
	ctx := r.Context()
	tx, id, err := a.accessTransactionLock(r, id, true)
	if err != nil {
		return 0, nil, false, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH active AS (
 SELECT id,status FROM export_jobs WHERE tenant_id=$1 AND actor_id=$2 AND ($3='' OR id=$3)
 AND status IN ('queued','running') FOR UPDATE
), cancelled AS (
 UPDATE export_jobs j SET status='cancelled',code='cancelled_by_operator' FROM active
 WHERE j.id=active.id RETURNING j.id,active.status
), logged AS (
 INSERT INTO audit_events(actor_id,tenant_id,action,resource_id)
 SELECT $2,$1,'export.cancelled',id FROM cancelled
)
SELECT id,status FROM cancelled`, tenant, id.User.ID, job)
	if err != nil {
		return 0, nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var jobID, status string
		if err := rows.Scan(&jobID, &status); err != nil {
			return 0, nil, false, err
		}
		count++
		if status == "running" {
			running = append(running, jobID)
		}
	}
	if rows.Err() != nil {
		return 0, nil, false, rows.Err()
	}
	if job != "" && count == 0 {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM export_jobs WHERE id=$1 AND actor_id=$2 AND tenant_id=$3)`, job, id.User.ID, tenant).Scan(&exists)
		return 0, nil, exists, err
	}
	return count, running, true, tx.Commit(ctx)
}
