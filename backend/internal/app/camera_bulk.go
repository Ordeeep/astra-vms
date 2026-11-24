package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type bulkCameraAgenda struct {
	Always  *bool            `json:"always"`
	Windows []ScheduleWindow `json:"windows"`
}

type bulkCameraInput struct {
	CameraIDs        []string          `json:"cameraIds"`
	Action           string            `json:"action"`
	SiteID           *string           `json:"siteId"`
	DisplayMode      *string           `json:"displayMode"`
	RecordingEnabled *bool             `json:"recordingEnabled"`
	Agenda           *bulkCameraAgenda `json:"agenda"`
}

type bulkCameraTarget struct {
	key, name, site, displayMode string
	active                       bool
	deleted                      *time.Time
}

type bulkCameraChange struct {
	target        bulkCameraTarget
	site, display string
	active        bool
	recording     *RecordingConfig
}

func (change bulkCameraChange) inventoryChanged() bool {
	return change.target.site != change.site || change.target.displayMode != change.display || change.target.active != change.active
}

func (a *App) bulkCameras(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in bulkCameraInput
	if !body(w, r, &in) {
		return
	}
	recording := in.RecordingEnabled != nil || in.Agenda != nil
	configure := in.SiteID != nil || in.DisplayMode != nil || recording
	switch in.Action {
	case "activate", "deactivate", "delete":
	case "configure":
	default:
		problem(w, 422, "invalid_camera_bulk")
		return
	}
	if len(in.CameraIDs) == 0 || (in.Action == "configure") != configure || (in.SiteID != nil && *in.SiteID == "") || !validCameraDisplayMode(in.DisplayMode) {
		problem(w, 422, "invalid_camera_bulk")
		return
	}
	if in.Agenda != nil && (in.Agenda.Always == nil || !validSchedule(in.Agenda.Windows) || (*in.Agenda.Always && len(in.Agenda.Windows) > 0)) {
		problem(w, 422, "invalid_camera_bulk_agenda")
		return
	}
	numbers := make([]int64, 0, len(in.CameraIDs))
	seen := make(map[int64]bool, len(in.CameraIDs))
	for _, ref := range in.CameraIDs {
		number := cameraNumber(ref)
		if number == 0 || seen[number] {
			problem(w, 422, "invalid_camera_bulk")
			return
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	if in.Action == "delete" {
		a.recordingMu.Lock()
		defer a.recordingMu.Unlock()
	}
	ctx := r.Context()
	tx, actor, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(ctx)
	grants, err := queryGrants(ctx, tx, actor.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	if in.SiteID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=$2)", tenant, *in.SiteID).Scan(&exists); err != nil {
			dbFailure(w)
			return
		}
		if !exists {
			problem(w, 404, "camera_bulk_site_not_found")
			return
		}
	}
	targets, err := loadBulkCameraTargets(ctx, tx, tenant, numbers)
	if err != nil {
		dbFailure(w)
		return
	}
	type issue struct {
		CameraID string `json:"cameraId"`
		Code     string `json:"code"`
	}
	issues := []issue{}
	changes := make([]bulkCameraChange, 0, len(numbers))
	for _, number := range numbers {
		change, code, err := planBulkCameraChange(ctx, tx, tenant, grants, in, targets[number])
		if err != nil {
			dbFailure(w)
			return
		}
		if code != "" {
			issues = append(issues, issue{strconv.FormatInt(number, 10), code})
			continue
		}
		changes = append(changes, change)
	}
	if len(issues) > 0 {
		response(w, 422, map[string]any{"code": "camera_bulk_rejected", "issues": issues})
		return
	}
	changed := 0
	var invalidate []string
	for _, change := range changes {
		camera := change.target
		if in.Action == "delete" {
			if camera.deleted != nil {
				continue
			}
			if deleteCameraInTx(ctx, tx, actor, tenant, camera.key, camera.name, camera.site) != nil {
				dbFailure(w)
				return
			}
			invalidate = append(invalidate, camera.key)
		} else {
			if !change.inventoryChanged() && change.recording == nil {
				continue
			}
			if applyBulkCameraConfiguration(ctx, tx, actor, tenant, change) != nil {
				dbFailure(w)
				return
			}
			if camera.active != change.active || camera.site != change.site {
				invalidate = append(invalidate, camera.key)
			}
		}
		changed++
	}
	if tx.Commit(ctx) != nil {
		dbFailure(w)
		return
	}
	for _, key := range invalidate {
		a.invalidateAvailability(tenant, key, "")
	}
	status := http.StatusOK
	if in.Action == "delete" {
		status = http.StatusAccepted
	}
	response(w, status, map[string]int{"changed": changed, "unchanged": len(changes) - changed})
}

func loadBulkCameraTargets(ctx context.Context, tx pgx.Tx, tenant string, numbers []int64) (map[int64]bulkCameraTarget, error) {
	rows, err := tx.Query(ctx, `SELECT camera_number,id,name,site_id,display_mode,active,deleted_at
FROM cameras WHERE tenant_id=$1 AND camera_number=ANY($2) ORDER BY camera_number FOR UPDATE`, tenant, numbers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make(map[int64]bulkCameraTarget, len(numbers))
	for rows.Next() {
		var number int64
		var target bulkCameraTarget
		if err := rows.Scan(&number, &target.key, &target.name, &target.site, &target.displayMode, &target.active, &target.deleted); err != nil {
			return nil, err
		}
		targets[number] = target
	}
	return targets, rows.Err()
}

func planBulkCameraChange(ctx context.Context, tx pgx.Tx, tenant string, grants []Grant, in bulkCameraInput, camera bulkCameraTarget) (bulkCameraChange, string, error) {
	change := bulkCameraChange{target: camera, site: camera.site, display: camera.displayMode, active: camera.active}
	if camera.key == "" || (camera.deleted != nil && in.Action != "delete") {
		return change, "camera_not_found", nil
	}
	if in.Action == "delete" {
		if !permits(grants, tenant, "camera.delete", camera.site, camera.key) {
			return change, "forbidden", nil
		}
		return change, "", nil
	}
	if in.Action != "configure" || in.SiteID != nil || in.DisplayMode != nil {
		if !permits(grants, tenant, "inventory.manage", camera.site, camera.key) {
			return change, "forbidden", nil
		}
	}
	if in.Action == "activate" || in.Action == "deactivate" {
		change.active = in.Action == "activate"
		return change, "", nil
	}
	if in.SiteID != nil && camera.site != *in.SiteID {
		if !permits(grants, tenant, "inventory.manage", *in.SiteID, "") {
			return change, "camera_bulk_site_forbidden", nil
		}
		change.site = *in.SiteID
	}
	if in.DisplayMode != nil {
		change.display = *in.DisplayMode
	}
	if in.RecordingEnabled == nil && in.Agenda == nil {
		return change, "", nil
	}
	if !permits(grants, tenant, "recording.manage", camera.site, camera.key) || !permits(grants, tenant, "recording.manage", change.site, camera.key) {
		return change, "camera_bulk_recording_forbidden", nil
	}
	config, err := scanRecording(tx.QueryRow(ctx, `SELECT enabled,profile,schedule,always_on,retention_days,revision
FROM recording_configs WHERE tenant_id=$1 AND camera_id=$2 FOR UPDATE`, tenant, camera.key))
	if errors.Is(err, pgx.ErrNoRows) {
		config = defaultCameraRecording()
	} else if err != nil {
		return change, "", err
	}
	previous := config
	if in.RecordingEnabled != nil {
		config.Enabled = *in.RecordingEnabled
	}
	if in.Agenda != nil {
		config.Always = *in.Agenda.Always
		config.Schedule = in.Agenda.Windows
	}
	if config.Enabled {
		valid, err := recordingProfileValid(ctx, tx, tenant, camera.key, config.Profile)
		if err != nil {
			return change, "", err
		}
		if !valid {
			return change, "recording_profile_unavailable", nil
		}
	}
	if config.Enabled != previous.Enabled || config.Always != previous.Always || (!config.Always && !slices.Equal(config.Schedule, previous.Schedule)) {
		change.recording = &config
	}
	return change, "", nil
}

func applyBulkCameraConfiguration(ctx context.Context, tx pgx.Tx, actor identity, tenant string, change bulkCameraChange) error {
	camera := change.target
	if change.inventoryChanged() {
		if _, err := tx.Exec(ctx, `UPDATE cameras SET active=$3,site_id=$4,display_mode=$5,revision=revision+1
WHERE tenant_id=$1 AND id=$2`, tenant, camera.key, change.active, change.site, change.display); err != nil {
			return err
		}
		if camera.site != change.site {
			if err := recordCameraLocation(ctx, tx, actor, tenant, camera.key, camera.site, change.site); err != nil {
				return err
			}
		}
		if err := audit(ctx, tx, actor, tenant, "camera.saved", camera.key, map[string]any{"siteId": change.site, "name": camera.name}); err != nil {
			return err
		}
	}
	if change.recording != nil {
		return storeCameraRecording(ctx, tx, actor, tenant, camera.key, *change.recording)
	}
	return nil
}
