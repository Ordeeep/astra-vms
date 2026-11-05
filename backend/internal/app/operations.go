package app

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// Audit responses deliberately expose a fixed field set. Internal details may
// grow as new integrations arrive; those fields are never copied wholesale.
func (a *App) audit(w http.ResponseWriter, r *http.Request, id identity) {
	tenant := ""
	if r.PathValue("tenant") != "" {
		var ok bool
		tenant, ok = a.tenantContext(w, r, id)
		if !ok {
			return
		}
		if !a.allowed(r.Context(), id, tenant, "audit.view", "", "") {
			problem(w, 403, "forbidden")
			return
		}
	} else if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	before := int64(9223372036854775807)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 1 {
			problem(w, 422, "invalid_cursor")
			return
		}
		before = n
	}
	action := r.URL.Query().Get("action")
	if len(action) > 100 {
		problem(w, 422, "invalid_filter")
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT e.id,e.actor_id,coalesce(o.username,'Sistema'),e.action,coalesce(e.resource_id,''),e.occurred_at,e.revision,coalesce(e.details->>'status',e.outcome),coalesce(e.details->>'code','')
 FROM audit_events e LEFT JOIN operators o ON o.id=e.actor_id
 WHERE (($1='' AND e.tenant_id IS NULL) OR e.tenant_id=$1) AND e.id<$2 AND ($3='' OR e.action=$3)
 ORDER BY e.id DESC LIMIT 101`, tenant, before, action)
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	cursor := ""
	for rows.Next() {
		var n int64
		var actor, name, action, resource string
		var at time.Time
		var revision *int64
		var outcome, reason string
		if rows.Scan(&n, &actor, &name, &action, &resource, &at, &revision, &outcome, &reason) != nil {
			dbFailure(w)
			return
		}
		if len(items) == 100 {
			cursor = items[99]["id"].(string)
			break
		}
		switch outcome {
		case "succeeded", "completed", "failed", "cancelled", "queued", "running", "expired":
		default:
			outcome = "recorded"
		}
		if !auditReason.MatchString(reason) {
			reason = ""
		}
		items = append(items, map[string]any{"id": strconv.FormatInt(n, 10), "actorId": actor, "actor": name, "action": action, "resourceId": resource, "occurredAt": at, "revision": revision, "outcome": outcome, "reason": reason})
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}

var auditReason = regexp.MustCompile(`^[a-z0-9_]{1,80}$`)

func (a *App) diagnostics(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "audit.view", "", "") && !a.allowed(r.Context(), id, tenant, "recording.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	var cameras, enabled int
	if a.db.QueryRow(ctx, "SELECT count(*),count(*) FILTER (WHERE active) FROM cameras WHERE tenant_id=$1 AND deleted_at IS NULL", tenant).Scan(&cameras, &enabled) != nil {
		dbFailure(w)
		return
	}
	var result struct {
		Configured bool `json:"configured"`
	}
	workerCtx, workerCancel := context.WithTimeout(ctx, 700*time.Millisecond)
	workerOK := a.worker(workerCtx, "GET", "/archive/config", nil, &result) == nil
	var usage map[string]int64
	if workerOK && result.Configured {
		workerOK = a.worker(workerCtx, "GET", "/archive/usage?tenant="+tenant, nil, &usage) == nil
	}
	workerCancel()
	rows, err := a.db.Query(ctx, `SELECT c.id,c.name,c.site_id,coalesce(s.code,'not_configured'),s.last_frame,s.last_write,s.updated_at
 FROM cameras c LEFT JOIN recording_status s ON s.tenant_id=c.tenant_id AND s.camera_id=c.id WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND c.id>$2 ORDER BY c.id LIMIT 201`, tenant, r.URL.Query().Get("cursor"))
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	cursor := ""
	for rows.Next() {
		var camera, name, site, code string
		var frame, write, updated *time.Time
		if rows.Scan(&camera, &name, &site, &code, &frame, &write, &updated) != nil {
			dbFailure(w)
			return
		}
		if len(items) == 200 {
			cursor = items[199]["cameraId"].(string)
			break
		}
		if !workerOK || (updated != nil && time.Since(*updated) > 5*time.Second) {
			code = "worker_unavailable"
		}
		items = append(items, map[string]any{"cameraId": camera, "name": name, "siteId": site, "code": code, "lastFrame": frame, "lastWrite": write, "updatedAt": updated})
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"database": "available", "workerAvailable": workerOK, "storageConfigured": result.Configured, "usage": usage, "cameraCount": cameras, "activeCameras": enabled, "observedAt": time.Now().UTC(), "items": items, "nextCursor": cursor})
}
func (a *App) recordStatus(ctx context.Context, tenant, camera, site, source, code string, frame, write *time.Time) {
	// Status changes and their origin snapshot are durable in the same statement.
	a.db.Exec(ctx, `WITH previous AS MATERIALIZED (SELECT code FROM recording_status WHERE tenant_id=$1 AND camera_id=$2),
 changed AS (INSERT INTO recording_status(tenant_id,camera_id,code,last_frame,last_write)
 SELECT $1,$2,$3,$4,$5 WHERE EXISTS(SELECT 1 FROM cameras WHERE tenant_id=$1 AND id=$2 AND site_id=$6 AND source_version=$7 AND deleted_at IS NULL)
 ON CONFLICT(tenant_id,camera_id) DO UPDATE SET code=$3,last_frame=coalesce($4,recording_status.last_frame),last_write=coalesce($5,recording_status.last_write),updated_at=now() RETURNING camera_id)
 INSERT INTO recording_events(tenant_id,camera_id,site_id,source_version,code)
 SELECT $1,changed.camera_id,$6,$7,$3 FROM changed
 WHERE $3 IS DISTINCT FROM (SELECT code FROM previous)`, tenant, camera, code, frame, write, site, source)
}
func (a *App) recordingEvents(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	before := int64(9223372036854775807)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 1 {
			problem(w, 422, "invalid_cursor")
			return
		}
		before = n
	}
	rows, err := a.db.Query(r.Context(), `SELECT e.id,e.camera_id,c.name,e.site_id,e.source_version,e.code,e.occurred_at FROM recording_events e JOIN cameras c ON c.tenant_id=e.tenant_id AND c.id=e.camera_id WHERE e.tenant_id=$1 AND e.id<$2 ORDER BY e.id DESC LIMIT 201`, tenant, before)
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	cursor := ""
	count := 0
	for rows.Next() {
		var n int64
		var camera, name, site, source, code string
		var at time.Time
		if rows.Scan(&n, &camera, &name, &site, &source, &code, &at) != nil {
			dbFailure(w)
			return
		}
		count++
		if count > 200 {
			break
		}
		cursor = strconv.FormatInt(n, 10)
		if !id.User.GlobalAdmin && !permits(grants, tenant, "audit.view", site, camera) && !permits(grants, tenant, "recording.manage", site, camera) {
			continue
		}
		items = append(items, map[string]any{"id": cursor, "cameraId": camera, "name": name, "siteId": site, "sourceVersion": source, "code": code, "occurredAt": at})
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	if count <= 200 {
		cursor = ""
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}
func (a *App) validateVault(ctx context.Context) error {
	rows, err := a.db.Query(ctx, "SELECT v.tenant_id,v.camera_id,v.id,v.secrets FROM camera_sources v JOIN cameras c ON c.tenant_id=v.tenant_id AND c.id=v.camera_id WHERE c.deleted_at IS NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenant, camera, source string
		var secret []byte
		if err = rows.Scan(&tenant, &camera, &source, &secret); err != nil {
			return err
		}
		plain, e := a.unseal(secret, tenant+"/"+camera+"/"+source)
		if e != nil {
			return e
		}
		if !json.Valid(plain) {
			return &json.SyntaxError{}
		}
	}
	return rows.Err()
}
func (a *App) registerOperations(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/audit", a.protected(a.audit))
	mux.HandleFunc("GET /api/v1/admin/audit", a.protected(a.audit))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/diagnostics", a.protected(a.diagnostics))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/recording-events", a.protected(a.recordingEvents))
}
