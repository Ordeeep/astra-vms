package app

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type RecordingUpdate struct {
	RecordingConfig
	ConfirmImpact bool `json:"confirmImpact"`
}

// The caller owns the authorization transaction. Camera/source and recording
// changes either commit together or leave the entire form unchanged.
func (a *App) saveCameraRecording(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id identity, tenant, cid, site string, in *RecordingUpdate) bool {
	if !allowedWith(r.Context(), tx, id, tenant, "recording.manage", site, cid) {
		problem(w, 403, "forbidden")
		return false
	}
	if !profileID.MatchString(in.Profile) || !validSchedule(in.Schedule) || (in.Retention != nil && (*in.Retention < 1 || *in.Retention > 3650)) {
		problem(w, 422, "invalid_recording_config")
		return false
	}
	if in.Enabled && !a.recordingProfileValid(w, r, tx, tenant, cid, in.Profile) {
		return false
	}
	previous, err := scanRecording(tx.QueryRow(r.Context(), "SELECT enabled,profile,schedule,always_on,retention_days,revision FROM recording_configs WHERE tenant_id=$1 AND camera_id=$2 FOR UPDATE", tenant, cid))
	if err != nil && err != pgx.ErrNoRows {
		dbFailure(w)
		return false
	}
	if previous.Revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return false
	}
	var defaultDays *int
	var n int
	err = tx.QueryRow(r.Context(), "SELECT retention_days FROM recording_policies WHERE tenant_id=$1", tenant).Scan(&n)
	if err == nil {
		defaultDays = &n
	} else if err != pgx.ErrNoRows {
		dbFailure(w)
		return false
	}
	oldDays, days := previous.Retention, in.Retention
	if oldDays == nil {
		oldDays = defaultDays
	}
	if days == nil {
		days = defaultDays
	}
	if days != nil && (oldDays == nil || *days < *oldDays) && !in.ConfirmImpact {
		var impact int64
		if tx.QueryRow(r.Context(), "SELECT coalesce(sum(charged),0) FROM archive_segments WHERE tenant_id=$1 AND camera_id=$2 AND available AND end_at<now()-make_interval(days=>$3)", tenant, cid, *days).Scan(&impact) != nil {
			dbFailure(w)
			return false
		}
		if impact > 0 {
			response(w, 409, map[string]any{"code": "retention_impact_confirmation", "eligibleBytes": impact})
			return false
		}
	}
	raw, _ := json.Marshal(in.Schedule)
	if in.Schedule == nil {
		raw = []byte("[]")
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO recording_configs(tenant_id,camera_id,enabled,profile,schedule,always_on,retention_days) VALUES($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT(tenant_id,camera_id) DO UPDATE SET enabled=$3,profile=$4,schedule=$5,always_on=$6,retention_days=$7,revision=recording_configs.revision+1`, tenant, cid, in.Enabled, in.Profile, raw, in.Always, in.Retention)
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,revision) SELECT $1,$2,'recording.configured',$3,revision FROM recording_configs WHERE tenant_id=$2 AND camera_id=$3", id.User.ID, tenant, cid)
	}
	if err != nil {
		dbFailure(w)
		return false
	}
	return true
}

func (a *App) recordingProfileValid(w http.ResponseWriter, r *http.Request, tx pgx.Tx, tenant, cid, profile string) bool {
	var valid bool
	err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cameras c JOIN camera_sources s ON s.tenant_id=c.tenant_id AND s.camera_id=c.id AND s.id=c.source_version, jsonb_array_elements(s.profiles) p WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL AND p->>'id'=$3 AND coalesce(p->>'enabled','true')<>'false')`, tenant, cid, profile).Scan(&valid)
	if err != nil {
		dbFailure(w)
		return false
	}
	if !valid {
		problem(w, 422, "recording_profile_unavailable")
	}
	return valid
}

// Also protect source edits made through legacy API endpoints or by operators
// who can edit inventory but cannot change an active recording configuration.
func (a *App) preserveRecordingProfile(w http.ResponseWriter, r *http.Request, tx pgx.Tx, tenant, cid string) bool {
	var invalid bool
	err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM recording_configs r JOIN cameras c ON c.tenant_id=r.tenant_id AND c.id=r.camera_id JOIN camera_sources s ON s.tenant_id=c.tenant_id AND s.camera_id=c.id AND s.id=c.source_version WHERE r.tenant_id=$1 AND r.camera_id=$2 AND r.enabled AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(s.profiles) p WHERE p->>'id'=r.profile AND coalesce(p->>'enabled','true')<>'false'))`, tenant, cid).Scan(&invalid)
	if err != nil {
		dbFailure(w)
		return false
	}
	if invalid {
		problem(w, 409, "recording_profile_change_required")
	}
	return !invalid
}
