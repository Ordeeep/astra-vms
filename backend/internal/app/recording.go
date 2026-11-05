package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/media"
	"vms.local/backend/internal/storage"
)

type ScheduleWindow struct {
	Day   int `json:"day"`   // Sunday = 0; an overnight range belongs to its start day.
	Start int `json:"start"` // minute of local day
	End   int `json:"end"`
}
type RecordingConfig struct {
	Enabled   bool             `json:"enabled"`
	Profile   string           `json:"profile"`
	Schedule  []ScheduleWindow `json:"schedule"`
	Always    bool             `json:"always"`
	Retention *int             `json:"retentionDays"`
	Revision  int64            `json:"revision"`
}
type RecordingPolicy struct {
	Quota        int64 `json:"quotaBytes"`
	ExportBudget int64 `json:"exportBytes"`
	Retention    int   `json:"retentionDays"`
	ExportHours  int   `json:"exportHours"`
	Revision     int64 `json:"revision"`
}

func scheduled(at time.Time, zone string, always bool, windows []ScheduleWindow) bool {
	if always {
		return true
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return false
	}
	local := at.In(loc)
	day, minute := int(local.Weekday()), local.Hour()*60+local.Minute()
	for _, w := range windows {
		if w.End > w.Start && day == w.Day && minute >= w.Start && minute < w.End {
			return true
		}
		if w.End < w.Start && ((day == w.Day && minute >= w.Start) || (day == (w.Day+1)%7 && minute < w.End)) {
			return true
		}
	}
	return false
}
func validSchedule(windows []ScheduleWindow) bool {
	if len(windows) > 56 {
		return false
	}
	for _, w := range windows {
		if w.Day < 0 || w.Day > 6 || w.Start < 0 || w.Start >= 1440 || w.End < 0 || w.End > 1440 || w.Start == w.End {
			return false
		}
	}
	return true
}
func scanRecording(row pgx.Row) (RecordingConfig, error) {
	var c RecordingConfig
	var raw []byte
	err := row.Scan(&c.Enabled, &c.Profile, &raw, &c.Always, &c.Retention, &c.Revision)
	if err == nil {
		err = json.Unmarshal(raw, &c.Schedule)
	}
	return c, err
}
func (a *App) recordingConfig(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	camera := r.PathValue("camera")
	var site string
	if a.db.QueryRow(r.Context(), "SELECT site_id FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL", tenant, camera).Scan(&site) != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !a.allowed(r.Context(), id, tenant, "recording.manage", site, camera) {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		c, err := scanRecording(a.db.QueryRow(r.Context(), "SELECT enabled,profile,schedule,always_on,retention_days,revision FROM recording_configs WHERE tenant_id=$1 AND camera_id=$2", tenant, camera))
		if err == pgx.ErrNoRows {
			c = RecordingConfig{Profile: "main", Always: true, Schedule: []ScheduleWindow{}}
		} else if err != nil {
			dbFailure(w)
			return
		}
		response(w, 200, c)
		return
	}
	var in struct {
		RecordingConfig
		ConfirmImpact bool `json:"confirmImpact"`
	}
	if !body(w, r, &in) {
		return
	}
	if !profileID.MatchString(in.Profile) || !validSchedule(in.Schedule) || (in.Retention != nil && (*in.Retention < 1 || *in.Retention > 3650)) {
		problem(w, 422, "invalid_recording_config")
		return
	}
	if _, err := a.cameraSource(r.Context(), tenant, camera, currentSource(a, r, tenant, camera), in.Profile); err != nil {
		problem(w, 422, "recording_profile_unavailable")
		return
	}
	tx, id, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	var source string
	if tx.QueryRow(r.Context(), "SELECT site_id,source_version FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL", tenant, camera).Scan(&site, &source) != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !allowedWith(r.Context(), tx, id, tenant, "recording.manage", site, camera) {
		problem(w, 403, "forbidden")
		return
	}
	var containsProfile bool
	if tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM camera_sources s,jsonb_array_elements(s.profiles) p WHERE s.tenant_id=$1 AND s.camera_id=$2 AND s.id=$3 AND p->>'id'=$4)`, tenant, camera, source, in.Profile).Scan(&containsProfile) != nil || !containsProfile {
		problem(w, 422, "recording_profile_unavailable")
		return
	}
	var revision int64
	err = tx.QueryRow(r.Context(), "SELECT revision FROM recording_configs WHERE tenant_id=$1 AND camera_id=$2 FOR UPDATE", tenant, camera).Scan(&revision)
	if err != nil && err != pgx.ErrNoRows {
		dbFailure(w)
		return
	}
	if revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	days := in.Retention
	if days == nil {
		var n int
		if tx.QueryRow(r.Context(), "SELECT retention_days FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&n) == nil {
			days = &n
		}
	}
	var impact int64
	if days != nil {
		tx.QueryRow(r.Context(), "SELECT coalesce(sum(charged),0) FROM archive_segments WHERE tenant_id=$1 AND camera_id=$2 AND available AND end_at<now()-make_interval(days=>$3)", tenant, camera, *days).Scan(&impact)
	}
	if impact > 0 && !in.ConfirmImpact {
		response(w, 409, map[string]any{"code": "retention_impact_confirmation", "eligibleBytes": impact})
		return
	}
	raw, _ := json.Marshal(in.Schedule)
	if in.Schedule == nil {
		raw = []byte("[]")
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO recording_configs(tenant_id,camera_id,enabled,profile,schedule,always_on,retention_days) VALUES($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT(tenant_id,camera_id) DO UPDATE SET enabled=$3,profile=$4,schedule=$5,always_on=$6,retention_days=$7,revision=recording_configs.revision+1`, tenant, camera, in.Enabled, in.Profile, raw, in.Always, in.Retention)
	if err != nil {
		dbFailure(w)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,revision) SELECT $1,$2,'recording.configured',$3,revision FROM recording_configs WHERE tenant_id=$2 AND camera_id=$3", id.User.ID, tenant, camera); err != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	in.Revision++
	response(w, 200, in.RecordingConfig)
}
func currentSource(a *App, r *http.Request, tenant, camera string) string {
	var source string
	a.db.QueryRow(r.Context(), "SELECT source_version FROM cameras WHERE tenant_id=$1 AND id=$2", tenant, camera).Scan(&source)
	return source
}
func (a *App) storageConfig(w http.ResponseWriter, r *http.Request, id identity) {
	if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		var out any
		if a.worker(r.Context(), "GET", "/archive/config", nil, &out) != nil {
			problem(w, 503, "storage_worker_unavailable")
			return
		}
		response(w, 200, out)
		return
	}
	var in media.ArchiveConfig
	if !body(w, r, &in) {
		return
	}
	if a.worker(r.Context(), "POST", "/archive/config", in, nil) != nil {
		problem(w, 422, "storage_configuration_rejected")
		return
	}
	raw, _ := json.Marshal(in)
	_, err := a.db.Exec(r.Context(), "INSERT INTO settings(key,value) VALUES('storage',$1) ON CONFLICT(key) DO UPDATE SET value=$1,revision=settings.revision+1", raw)
	if err != nil {
		dbFailure(w)
		return
	}
	a.db.Exec(r.Context(), "INSERT INTO audit_events(actor_id,action,resource_id) VALUES($1,'storage.configured','storage')", id.User.ID)
	response(w, 200, in)
}
func (a *App) recordingPolicy(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "recording.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	var policy RecordingPolicy
	err := a.db.QueryRow(r.Context(), "SELECT quota_bytes,export_bytes,retention_days,export_hours,revision FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&policy.Quota, &policy.ExportBudget, &policy.Retention, &policy.ExportHours, &policy.Revision)
	if err == pgx.ErrNoRows {
		policy = RecordingPolicy{Retention: 10, ExportHours: 24}
	} else if err != nil {
		dbFailure(w)
		return
	}
	if r.Method == "GET" {
		var usage map[string]int64
		a.worker(r.Context(), "GET", "/archive/usage?tenant="+url.QueryEscape(tenant), nil, &usage)
		response(w, 200, map[string]any{"policy": policy, "usage": usage})
		return
	}
	var in struct {
		RecordingPolicy
		ConfirmImpact bool `json:"confirmImpact"`
	}
	if !body(w, r, &in) {
		return
	}
	if in.Quota < 0 || in.ExportBudget < 0 || in.Retention < 1 || in.Retention > 3650 || in.ExportHours < 1 || in.ExportHours > 720 {
		problem(w, 422, "invalid_storage_policy")
		return
	}
	if in.Revision != policy.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	a.recordingMu.Lock()
	defer a.recordingMu.Unlock()
	if a.reductionDirty[tenant] {
		problem(w, 503, "storage_authority_unavailable")
		return
	}
	if a.quotaDirty {
		if !a.reconcileQuotaAuthority() {
			problem(w, 503, "storage_authority_unavailable")
			return
		}
		a.quotaDirty = false
	}
	// Changing hard quotas is a separate job when existing usage must be freed.
	var usage map[string]int64
	if a.worker(r.Context(), "GET", "/archive/usage?tenant="+url.QueryEscape(tenant), nil, &usage) != nil {
		problem(w, 503, "storage_unavailable")
		return
	}
	if in.Quota < usage["recording"] || in.ExportBudget < usage["export"] {
		response(w, 409, map[string]any{"code": "quota_reduction_required", "usage": usage})
		return
	}
	var impact int64
	if err = a.db.QueryRow(r.Context(), `SELECT coalesce(sum(s.charged),0) FROM archive_segments s LEFT JOIN recording_configs c ON c.tenant_id=s.tenant_id AND c.camera_id=s.camera_id WHERE s.tenant_id=$1 AND s.available AND c.retention_days IS NULL AND s.end_at<now()-make_interval(days=>$2)`, tenant, in.Retention).Scan(&impact); err != nil {
		dbFailure(w)
		return
	}
	if impact > 0 && !in.ConfirmImpact {
		response(w, 409, map[string]any{"code": "retention_impact_confirmation", "eligibleBytes": impact})
		return
	}
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
	active, err := activeQuota(r.Context(), tx, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	if active {
		problem(w, 409, "quota_job_active")
		return
	}
	var revision int64
	err = tx.QueryRow(r.Context(), "SELECT revision FROM recording_policies WHERE tenant_id=$1 FOR UPDATE", tenant).Scan(&revision)
	if err != nil && err != pgx.ErrNoRows {
		dbFailure(w)
		return
	}
	if revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	if a.worker(r.Context(), "DELETE", "/captures?tenant="+url.QueryEscape(tenant), nil, nil) != nil {
		problem(w, 503, "capture_stop_failed")
		return
	}
	if in.ExportBudget != policy.ExportBudget && a.worker(r.Context(), "DELETE", "/archive/exports?tenant="+url.QueryEscape(tenant), nil, nil) != nil {
		problem(w, 503, "export_stop_failed")
		return
	}
	applied, committed := false, false
	defer func() {
		if applied && !committed {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			a.worker(ctx, "PUT", "/archive/limits", map[string]any{"tenant": tenant, "kind": "recording", "bytes": policy.Quota}, nil)
			a.worker(ctx, "PUT", "/archive/limits", map[string]any{"tenant": tenant, "kind": "export", "bytes": policy.ExportBudget}, nil)
		}
	}()
	for _, q := range []struct {
		kind string
		n    int64
	}{{"recording", in.Quota}, {"export", in.ExportBudget}} {
		// A lost HTTP response does not mean the quota was not applied.
		a.quotaDirty = true
		a.quotaDirtyTenants[tenant] = true
		if a.worker(r.Context(), "PUT", "/archive/limits", map[string]any{"tenant": tenant, "kind": q.kind, "bytes": q.n}, nil) != nil {
			problem(w, 409, "quota_changed_retry")
			return
		}
		applied = true
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO recording_policies(tenant_id,quota_bytes,export_bytes,retention_days,export_hours) VALUES($1,$2,$3,$4,$5)
	ON CONFLICT(tenant_id) DO UPDATE SET quota_bytes=$2,export_bytes=$3,retention_days=$4,export_hours=$5,revision=recording_policies.revision+1`, tenant, in.Quota, in.ExportBudget, in.Retention, in.ExportHours)
	if err != nil {
		dbFailure(w)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,revision) SELECT $1,$2,'recording.policy_changed',$2,revision FROM recording_policies WHERE tenant_id=$2", id.User.ID, tenant); err != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		a.cancel()
		dbFailure(w)
		return
	}
	committed = true
	a.quotaDirty = false
	delete(a.quotaDirtyTenants, tenant)
	in.Revision++
	response(w, 200, in.RecordingPolicy)
}

type desiredCapture struct {
	tenant, camera, site, source, profile, zone string
	revision                                    int64
	connectionRevision                          int64
	enabled, active, always                     bool
	schedule                                    []ScheduleWindow
}

func (d desiredCapture) key() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/%d/%d/%s", d.tenant, d.camera, d.site, d.source, d.profile, d.revision, d.connectionRevision, d.zone)
}
func (a *App) recordingLoop() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	plans := map[string]media.CapturePlan{}
	reconciled := false
	for {
		select {
		case <-a.runContext.Done():
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			for _, p := range plans {
				a.worker(ctx, "DELETE", "/captures/"+p.ID, nil, nil)
			}
			cancel()
			return
		case <-ticker.C:
		}
		a.recordingMu.Lock()
		a.reconcileRecordingReductions()
		if !reconciled || a.quotaDirty {
			reconciled = a.reconcileQuotaAuthority()
			if !reconciled {
				a.recordingMu.Unlock()
				continue
			}
			a.quotaDirty = false
		}
		deadline := time.Now().Add(1400 * time.Millisecond)
		ctx, cancel := context.WithDeadline(a.runContext, deadline)
		rows, err := a.db.Query(ctx, `SELECT c.tenant_id,c.id,c.site_id,c.source_version,r.profile,s.timezone,r.revision,r.enabled,
		(c.active AND s.active AND t.active AND NOT EXISTS(SELECT 1 FROM quota_jobs q WHERE q.tenant_id=c.tenant_id AND q.status IN ('queued','applying','waiting'))),r.always_on,r.schedule,v.connection_revision FROM recording_configs r JOIN cameras c ON c.id=r.camera_id AND c.tenant_id=r.tenant_id
		JOIN sites s ON s.id=c.site_id AND s.tenant_id=c.tenant_id JOIN tenants t ON t.id=c.tenant_id
		JOIN camera_sources v ON v.tenant_id=c.tenant_id AND v.camera_id=c.id AND v.id=c.source_version
		JOIN recording_policies p ON p.tenant_id=c.tenant_id WHERE c.deleted_at IS NULL`)
		if err != nil {
			cancel()
			a.recordingMu.Unlock()
			continue
		}
		desired := []desiredCapture{}
		for rows.Next() {
			var d desiredCapture
			var raw []byte
			if rows.Scan(&d.tenant, &d.camera, &d.site, &d.source, &d.profile, &d.zone, &d.revision, &d.enabled, &d.active, &d.always, &raw, &d.connectionRevision) == nil && json.Unmarshal(raw, &d.schedule) == nil {
				desired = append(desired, d)
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil {
			cancel()
			a.recordingMu.Unlock()
			continue
		}
		next := map[string]media.CapturePlan{}
		for _, d := range desired {
			code := "outside_schedule"
			if a.reductionDirty[d.tenant] {
				code = "quota_transition"
			} else if !d.enabled {
				code = "recording_disabled"
			} else if !d.active {
				code = "camera_inactive"
			} else if scheduled(time.Now(), d.zone, d.always, d.schedule) {
				key := d.key()
				p, ok := plans[key]
				if !ok {
					source, e := a.cameraSource(ctx, d.tenant, d.camera, d.source, d.profile)
					if e != nil {
						code = "profile_unavailable"
					} else {
						p = media.CapturePlan{ConnectionRevision: d.connectionRevision, ID: newID(), Tenant: d.tenant, Camera: d.camera, Site: d.site, Source: d.source, Profile: d.profile, URL: source}
					}
				}
				if p.ID != "" {
					p.Expires = deadline
					if a.worker(ctx, "POST", "/captures", p, nil) == nil {
						next[key] = p
						code = "connecting"
					} else {
						code = "capture_unavailable"
					}
				}
			}
			if code != "connecting" {
				a.recordStatus(ctx, d.tenant, d.camera, d.site, d.source, code, nil, nil)
			}
		}
		for key, p := range plans {
			if _, ok := next[key]; !ok {
				a.worker(ctx, "DELETE", "/captures/"+p.ID, nil, nil)
			}
		}
		plans = next
		var statuses []media.CaptureStatus
		if a.worker(ctx, "GET", "/captures", nil, &statuses) == nil {
			byID := map[string]media.CapturePlan{}
			for _, p := range plans {
				byID[p.ID] = p
			}
			for _, s := range statuses {
				if p, ok := byID[s.ID]; ok {
					var frame, write *time.Time
					if !s.LastFrame.IsZero() {
						frame = &s.LastFrame
					}
					if !s.LastWrite.IsZero() {
						write = &s.LastWrite
					}
					a.recordStatus(ctx, p.Tenant, p.Camera, p.Site, p.Source, s.Code, frame, write)
				}
			}
		}
		cancel()
		a.recordingMu.Unlock()
	}
}
func (a *App) archiveMaintenance() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var cursor int64
	if err := a.db.QueryRow(a.runContext, "SELECT coalesce((SELECT (value#>>'{}')::bigint FROM settings WHERE key='archive_cursor'),0)").Scan(&cursor); err != nil {
		a.cancel()
		return
	}
	cycle := 0
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
		cycle++
		{
			scanCtx, done := context.WithTimeout(a.runContext, time.Second)
			var entries []storage.Entry
			if a.worker(scanCtx, "GET", "/archive/catalog?after="+strconv.FormatInt(cursor, 10), nil, &entries) == nil {
				for _, e := range entries {
					if e.Deleted {
						if _, err := a.db.Exec(scanCtx, "UPDATE archive_segments SET available=false,discard_reason=coalesce(discard_reason,'storage_reclaimed') WHERE id=$1 AND tenant_id=$2", e.ID, e.Tenant); err != nil {
							break
						}
						cursor = e.Sequence
						continue
					}
					if e.Kind != storage.Recording {
						cursor = e.Sequence
						continue
					}
					var index media.SegmentIndex
					if json.Unmarshal(e.Metadata, &index) != nil {
						break
					}
					_, err := a.db.Exec(scanCtx, `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata)
					SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11 FROM cameras WHERE tenant_id=$2 AND id=$3 AND deleted_at IS NULL ON CONFLICT(id) DO NOTHING`, e.ID, index.Tenant, index.Camera, index.Site, index.Source, index.Profile, index.Start, index.End, e.Size, e.Charged, e.Metadata)
					if err != nil {
						break
					}
					cursor = e.Sequence
				}
				if cursor > 0 {
					if _, err := a.db.Exec(scanCtx, "INSERT INTO settings(key,value) VALUES('archive_cursor',to_jsonb($1::bigint)) ON CONFLICT(key) DO UPDATE SET value=excluded.value", cursor); err == nil {
						a.worker(scanCtx, "POST", "/archive/catalog/ack", map[string]int64{"sequence": cursor}, nil)
					}
				}
			}
			done()
		}
		if cycle%15 == 0 {
			a.expireRecordings()
		}
	}
}

// A restarted core reconciles the durable SQL policy before authorizing any
// new capture. This closes the crash window between worker quota application
// and the SQL commit (including an uncertain commit result).
func (a *App) reconcileQuotaAuthority() bool {
	ctx, cancel := context.WithTimeout(a.runContext, 5*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx, "SELECT tenant_id,quota_bytes,export_bytes FROM recording_policies")
	if err != nil {
		return false
	}
	type quota struct {
		tenant            string
		recording, export int64
	}
	all := []quota{}
	for rows.Next() {
		var q quota
		if rows.Scan(&q.tenant, &q.recording, &q.export) != nil {
			rows.Close()
			return false
		}
		all = append(all, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false
	}
	for _, q := range all {
		if len(a.quotaDirtyTenants) > 0 && !a.quotaDirtyTenants[q.tenant] {
			continue
		}
		if a.worker(ctx, "DELETE", "/captures?tenant="+url.QueryEscape(q.tenant), nil, nil) != nil {
			return false
		}
		if a.worker(ctx, "DELETE", "/archive/exports?tenant="+url.QueryEscape(q.tenant), nil, nil) != nil {
			return false
		}
		for _, v := range []struct {
			kind string
			n    int64
		}{{"recording", q.recording}, {"export", q.export}} {
			if a.worker(ctx, "PUT", "/archive/limits", map[string]any{"tenant": q.tenant, "kind": v.kind, "bytes": v.n}, nil) != nil {
				return false
			}
		}
	}
	clear(a.quotaDirtyTenants)
	return true
}
func (a *App) expireRecordings() {
	ctx, cancel := context.WithTimeout(a.runContext, 2*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx, `SELECT s.id,s.tenant_id FROM archive_segments s JOIN recording_policies p ON p.tenant_id=s.tenant_id LEFT JOIN recording_configs c ON c.tenant_id=s.tenant_id AND c.camera_id=s.camera_id WHERE s.available AND s.end_at<now()-make_interval(days=>coalesce(c.retention_days,p.retention_days)) ORDER BY s.end_at LIMIT 100`)
	if err != nil {
		return
	}
	type item struct{ id, tenant string }
	items := []item{}
	for rows.Next() {
		var i item
		if rows.Scan(&i.id, &i.tenant) == nil {
			items = append(items, i)
		}
	}
	rows.Close()
	for _, i := range items {
		if a.worker(ctx, "DELETE", "/archive/objects/"+i.id+"?tenant="+url.QueryEscape(i.tenant), nil, nil) == nil {
			a.db.Exec(ctx, "UPDATE archive_segments SET available=false,discard_reason='retention' WHERE id=$1", i.id)
		}
	}
}
func (a *App) recordingStatuses(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT c.id,c.site_id,coalesce(s.code,'not_configured'),s.last_frame,s.last_write,s.updated_at FROM cameras c LEFT JOIN recording_status s ON s.tenant_id=c.tenant_id AND s.camera_id=c.id WHERE c.tenant_id=$1 AND c.deleted_at IS NULL ORDER BY c.id LIMIT 200`, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var camera, site, code string
		var frame, write, updated *time.Time
		if rows.Scan(&camera, &site, &code, &frame, &write, &updated) != nil {
			dbFailure(w)
			return
		}
		if !id.User.GlobalAdmin && !permits(grants, tenant, "recording.manage", site, camera) && !permits(grants, tenant, "playback", site, camera) {
			continue
		}
		if updated != nil && time.Since(*updated) > 5*time.Second {
			code = "worker_unavailable"
		}
		items = append(items, map[string]any{"cameraId": camera, "code": code, "lastFrame": frame, "lastWrite": write, "updatedAt": updated})
	}
	response(w, 200, map[string]any{"items": items})
}
func (a *App) registerRecording(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST"} {
		mux.HandleFunc(method+" /api/v1/admin/storage", a.protected(a.storageConfig))
	}
	for _, method := range []string{"GET", "PUT"} {
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/recording-policy", a.protected(a.recordingPolicy))
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/cameras/{camera}/recording", a.protected(a.recordingConfig))
	}
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/recording-status", a.protected(a.recordingStatuses))
}
