package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/storage"
)

type quotaJob struct {
	ID       string            `json:"id"`
	Tenant   string            `json:"-"`
	Actor    string            `json:"requestedBy"`
	Target   int64             `json:"targetBytes"`
	Revision int64             `json:"policyRevision"`
	Status   string            `json:"status"`
	Code     string            `json:"code"`
	Result   storage.Reduction `json:"result"`
	Created  time.Time         `json:"createdAt"`
	Expires  time.Time         `json:"expires"`
}

const quotaSelect = "SELECT id,tenant_id,actor_id,target_bytes,policy_revision,status,code,result,created_at,expires FROM quota_jobs"

func scanQuota(row pgx.Row) (quotaJob, error) {
	var j quotaJob
	var raw []byte
	err := row.Scan(&j.ID, &j.Tenant, &j.Actor, &j.Target, &j.Revision, &j.Status, &j.Code, &raw, &j.Created, &j.Expires)
	if err == nil {
		err = json.Unmarshal(raw, &j.Result)
	}
	return j, err
}
func activeQuota(ctx context.Context, db permissionReader, tenant string) (bool, error) {
	var active bool
	err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM quota_jobs WHERE tenant_id=$1 AND status IN ('queued','applying','waiting'))", tenant).Scan(&active)
	return active, err
}
func (a *App) quotaPreview(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "recording.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	var in struct {
		Target   int64 `json:"targetBytes"`
		Revision int64 `json:"revision"`
	}
	if !body(w, r, &in) {
		return
	}
	a.recordingMu.Lock()
	defer a.recordingMu.Unlock()
	var limit, revision int64
	if a.db.QueryRow(r.Context(), "SELECT quota_bytes,revision FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&limit, &revision) != nil {
		problem(w, 409, "storage_policy_required")
		return
	}
	if in.Revision != revision {
		problem(w, 412, "revision_conflict")
		return
	}
	if in.Target < 0 || in.Target >= limit {
		problem(w, 422, "invalid_reduction")
		return
	}
	if a.quotaDirty || a.reductionDirty[tenant] {
		problem(w, 503, "storage_authority_unavailable")
		return
	}
	var result storage.Reduction
	if a.worker(r.Context(), "POST", "/archive/reduction-preview", map[string]any{"tenant": tenant, "target": in.Target}, &result) != nil {
		problem(w, 503, "storage_unavailable")
		return
	}
	raw, _ := json.Marshal(result)
	j, err := scanQuota(a.db.QueryRow(r.Context(), `INSERT INTO quota_jobs(id,tenant_id,actor_id,target_bytes,policy_revision,result) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,tenant_id,actor_id,target_bytes,policy_revision,status,code,result,created_at,expires`, newID(), tenant, id.User.ID, in.Target, revision, raw))
	if err != nil {
		dbFailure(w)
		return
	}
	response(w, 201, j)
}
func (a *App) quotaJobs(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "recording.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		rows, err := a.db.Query(r.Context(), quotaSelect+" WHERE tenant_id=$1 AND status<>'preview' ORDER BY created_at DESC LIMIT 50", tenant)
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		items := []quotaJob{}
		for rows.Next() {
			j, e := scanQuota(rows)
			if e != nil {
				dbFailure(w)
				return
			}
			items = append(items, j)
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		response(w, 200, map[string]any{"items": items})
		return
	}
	var in struct {
		Preview string `json:"previewId"`
		Key     string `json:"idempotencyKey"`
	}
	if !body(w, r, &in) {
		return
	}
	if len(in.Key) < 16 || len(in.Key) > 128 {
		problem(w, 422, "invalid_idempotency")
		return
	}
	a.recordingMu.Lock()
	defer a.recordingMu.Unlock()
	tx, id, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	if !allowedWith(r.Context(), tx, id, tenant, "recording.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	existing, e := scanQuota(tx.QueryRow(r.Context(), quotaSelect+" WHERE tenant_id=$1 AND actor_id=$2 AND idempotency_key=$3", tenant, id.User.ID, in.Key))
	if e == nil {
		if existing.ID != in.Preview {
			problem(w, 409, "idempotency_conflict")
			return
		}
		response(w, 202, existing)
		return
	}
	if e != pgx.ErrNoRows {
		dbFailure(w)
		return
	}
	j, e := scanQuota(tx.QueryRow(r.Context(), quotaSelect+" WHERE id=$1 AND tenant_id=$2 AND actor_id=$3", in.Preview, tenant, id.User.ID))
	if e != nil || j.Status != "preview" || !time.Now().Before(j.Expires) {
		problem(w, 409, "quota_preview_expired")
		return
	}
	var revision int64
	if tx.QueryRow(r.Context(), "SELECT revision FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&revision) != nil {
		dbFailure(w)
		return
	}
	if revision != j.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	active, e := activeQuota(r.Context(), tx, tenant)
	if e != nil {
		dbFailure(w)
		return
	}
	if active {
		problem(w, 409, "quota_job_active")
		return
	}
	if _, e = tx.Exec(r.Context(), "UPDATE quota_jobs SET status='queued',idempotency_key=$2 WHERE id=$1", j.ID, in.Key); e != nil {
		dbFailure(w)
		return
	}
	if _, e = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,details) VALUES($1,$2,'quota.reduction_requested',$3,$4)", id.User.ID, tenant, j.ID, []byte(`{"targetBytes":`+jsonNumber(j.Target)+`}`)); e != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	j.Status = "queued"
	response(w, 202, j)
}
func jsonNumber(n int64) string { raw, _ := json.Marshal(n); return string(raw) }

func (a *App) quotaLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
		a.recordingMu.Lock()
		if !a.quotaDirty {
			a.stepQuota()
		}
		a.recordingMu.Unlock()
	}
}
func (a *App) stepQuota() {
	ctx, cancel := context.WithTimeout(a.runContext, 1200*time.Millisecond)
	defer cancel()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(78921452)"); err != nil {
		return
	}
	dirty := make([]string, 0, len(a.reductionDirty))
	for tenant := range a.reductionDirty {
		dirty = append(dirty, tenant)
	}
	j, err := scanQuota(tx.QueryRow(ctx, quotaSelect+" WHERE status IN ('queued','applying','waiting') AND NOT(tenant_id=ANY($1)) ORDER BY last_attempt,created_at,id LIMIT 1", dirty))
	if err != nil {
		return
	}
	var user User
	var tenantActive bool
	err = tx.QueryRow(ctx, "SELECT o.id,o.active,o.global_admin,t.active FROM operators o CROSS JOIN tenants t WHERE o.id=$1 AND t.id=$2", j.Actor, j.Tenant).Scan(&user.ID, &user.Active, &user.GlobalAdmin, &tenantActive)
	if err != nil || !user.Active || !tenantActive || !allowedWith(ctx, tx, identity{User: user}, j.Tenant, "recording.manage", "", "") {
		tx.Exec(ctx, "UPDATE quota_jobs SET status='failed',code='authority_revoked' WHERE id=$1", j.ID)
		tx.Commit(ctx)
		return
	}
	var revision int64
	if tx.QueryRow(ctx, "SELECT revision FROM recording_policies WHERE tenant_id=$1", j.Tenant).Scan(&revision) != nil {
		return
	}
	if revision != j.Revision {
		tx.Exec(ctx, "UPDATE quota_jobs SET status='failed',code='revision_conflict' WHERE id=$1", j.ID)
		tx.Commit(ctx)
		return
	}
	if a.worker(ctx, "DELETE", "/captures?tenant="+url.QueryEscape(j.Tenant), nil, nil) != nil {
		return
	}
	var result storage.Reduction
	// The response can be lost after the durable worker limit changed. Until a
	// positive result and SQL commit, capture must reconcile the old authority.
	a.reductionDirty[j.Tenant] = true
	if a.worker(ctx, "POST", "/archive/reductions", map[string]any{"tenant": j.Tenant, "target": j.Target, "expires": time.Now().Add(time.Second)}, &result) != nil {
		return
	}
	status, code := "applying", ""
	if result.Protected {
		status, code = "waiting", "protected_history"
	}
	if result.Applied {
		status = "completed"
		if _, err = tx.Exec(ctx, "UPDATE recording_policies SET quota_bytes=$2,revision=revision+1 WHERE tenant_id=$1", j.Tenant, j.Target); err != nil {
			return
		}
		if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) VALUES($1,$2,'quota.reduction_applied',$3)", j.Actor, j.Tenant, j.ID); err != nil {
			return
		}
	}
	raw, _ := json.Marshal(result)
	if _, err = tx.Exec(ctx, "UPDATE quota_jobs SET status=$2,code=$3,result=$4,last_attempt=now() WHERE id=$1", j.ID, status, code, raw); err != nil {
		return
	}
	if tx.Commit(ctx) != nil {
		a.cancel()
		return
	}
	delete(a.reductionDirty, j.Tenant)
}

// Only recording admission for the affected tenant is reconciled. An
// administrative quota failure must not release export/playback protections.
func (a *App) reconcileRecordingReductions() {
	for tenant := range a.reductionDirty {
		ctx, cancel := context.WithTimeout(a.runContext, 700*time.Millisecond)
		var limit int64
		err := a.db.QueryRow(ctx, "SELECT quota_bytes FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&limit)
		if err == nil {
			err = a.worker(ctx, "DELETE", "/captures?tenant="+url.QueryEscape(tenant), nil, nil)
		}
		if err == nil {
			err = a.worker(ctx, "PUT", "/archive/limits", map[string]any{"tenant": tenant, "kind": "recording", "bytes": limit}, nil)
		}
		if err == nil {
			delete(a.reductionDirty, tenant)
		}
		cancel()
	}
}
func (a *App) registerQuota(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/quota-previews", a.protected(a.quotaPreview))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/quota-jobs", a.protected(a.quotaJobs))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/quota-jobs", a.protected(a.quotaJobs))
}
