package app

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/media"
)

const availabilityInterval = 30 * time.Second

// Covers the next 30s cycle plus its bounded worker request, with margin.
const availabilityValidity = 65 * time.Second

type CameraAvailability struct {
	State       string                   `json:"state"`
	Code        string                   `json:"code,omitempty"`
	CheckedAt   *time.Time               `json:"checkedAt,omitempty"`
	LastVideoAt *time.Time               `json:"lastVideoAt,omitempty"`
	ValidUntil  *time.Time               `json:"validUntil,omitempty"`
	Profile     string                   `json:"profile,omitempty"`
	Warnings    []media.VideoObservation `json:"warnings,omitempty"`
}
type availabilityTarget struct {
	tenant string
	camera Camera
}
type availabilityEntry struct {
	target    availabilityTarget
	signature string
	next      time.Time
	running   bool
	cancel    context.CancelFunc
	failures  int
	status    CameraAvailability
	warnings  map[string]media.VideoObservation
}
type availabilityMonitor struct {
	mu      sync.Mutex
	entries map[string]*availabilityEntry
	synced  time.Time
}

func availabilityKey(tenant, camera string) string { return tenant + "/" + camera }
func availabilitySignature(c Camera) string {
	profiles, _ := json.Marshal(c.Profiles)
	return c.SourceVersion + "/" + strconv.FormatInt(c.ConnectionRevision, 10) + "/" + digest(string(profiles))
}
func newAvailabilityMonitor() *availabilityMonitor {
	return &availabilityMonitor{entries: map[string]*availabilityEntry{}}
}
func unknownAvailability(code string) CameraAvailability {
	return CameraAvailability{State: "unknown", Code: code}
}
func (m *availabilityMonitor) reconcile(targets []availabilityTarget, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	present := map[string]bool{}
	for _, target := range targets {
		key := availabilityKey(target.tenant, target.camera.ID)
		present[key] = true
		signature := availabilitySignature(target.camera)
		e := m.entries[key]
		if e == nil || e.signature != signature || e.target.camera.EffectiveActive != target.camera.EffectiveActive {
			if e != nil && e.cancel != nil {
				e.cancel()
			}
			e = &availabilityEntry{target: target, signature: signature, next: now, status: unknownAvailability("awaiting_check"), warnings: map[string]media.VideoObservation{}}
			if !target.camera.EffectiveActive {
				e.status = CameraAvailability{State: "suspended", Code: "camera_inactive"}
			}
			m.entries[key] = e
		} else {
			e.target = target
		}
	}
	for key, e := range m.entries {
		if !present[key] {
			if e.cancel != nil {
				e.cancel()
			}
			delete(m.entries, key)
		}
	}
	m.synced = now
}
func (m *availabilityMonitor) snapshot(tenant string, c Camera, now time.Time) CameraAvailability {
	if !c.EffectiveActive {
		return CameraAvailability{State: "suspended", Code: "camera_inactive"}
	}
	if m == nil {
		return unknownAvailability("awaiting_check")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[availabilityKey(tenant, c.ID)]
	if e == nil || e.signature != availabilitySignature(c) || !e.target.camera.EffectiveActive {
		return unknownAvailability("awaiting_check")
	}
	if now.Sub(m.synced) > 5*time.Second {
		status := e.status
		status.State = "unknown"
		status.Code = "monitor_unavailable"
		status.Profile = ""
		status.Warnings = nil
		return status
	}
	status := e.status
	status.Warnings = append([]media.VideoObservation(nil), status.Warnings...)
	if status.ValidUntil != nil && !now.Before(*status.ValidUntil) {
		status.State = "unknown"
		status.Code = "stale_check"
		status.Profile = ""
		status.Warnings = nil
	}
	return status
}
func (m *availabilityMonitor) apply(e *availabilityEntry, result media.AvailabilityResult, code string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[availabilityKey(e.target.tenant, e.target.camera.ID)] != e {
		return
	}
	status := e.status
	if status.ValidUntil != nil && !now.Before(*status.ValidUntil) {
		e.failures = 0
		status.State = "unknown"
	}
	status.CheckedAt = &now
	valid := now.Add(availabilityValidity)
	status.ValidUntil = &valid
	complete := map[string]bool{}
	positive := false
	for _, observation := range result.Profiles {
		if !knownAvailabilityCode(observation.Code) {
			observation.Code = "video_unavailable"
		}
		known := false
		for _, p := range e.target.camera.Profiles {
			if p.ID == observation.Profile && (p.Enabled == nil || *p.Enabled) {
				known = true
				break
			}
		}
		if !known || !observation.Complete {
			continue
		}
		if observation.Available && observation.LastVideoAt != nil && !observation.LastVideoAt.IsZero() && now.Sub(*observation.LastVideoAt) >= -time.Second && now.Sub(*observation.LastVideoAt) <= 30*time.Second {
			positive = true
			status.State = "online"
			status.Code = ""
			status.Profile = observation.Profile
			status.LastVideoAt = observation.LastVideoAt
			delete(e.warnings, observation.Profile)
		} else if !observation.Available {
			complete[observation.Profile] = true
			observation.CheckedAt = now
			e.warnings[observation.Profile] = observation
		}
	}
	if positive {
		e.failures = 0
	} else {
		all := code == ""
		profiles := 0
		for _, p := range e.target.camera.Profiles {
			if p.Enabled != nil && !*p.Enabled {
				continue
			}
			profiles++
			all = all && complete[p.ID]
		}
		all = all && profiles > 0
		if all {
			e.failures++
			status.Code = "video_unavailable"
			if e.failures >= 2 {
				status.State = "offline"
				status.Profile = ""
			} else if status.State != "online" && status.State != "offline" {
				status.State = "unknown"
				status.Code = "confirming_failure"
			}
		} else {
			e.failures = 0
			status.State = "unknown"
			status.Profile = ""
			if !knownAvailabilityCode(code) || code == "" {
				code = "check_incomplete"
			}
			status.Code = code
		}
	}
	status.Warnings = nil
	for _, warning := range e.warnings {
		status.Warnings = append(status.Warnings, warning)
	}
	sort.Slice(status.Warnings, func(i, j int) bool { return status.Warnings[i].Profile < status.Warnings[j].Profile })
	e.status = status
}
func knownAvailabilityCode(code string) bool {
	switch code {
	case "", "connection_failed", "authentication_failed", "description_failed", "setup_failed", "play_failed", "video_timeout", "video_unavailable", "unsupported_video", "invalid_configuration", "profiles_unavailable", "worker_unavailable", "monitor_capacity", "check_incomplete", "vault_unavailable":
		return true
	}
	return false
}
func (a *App) invalidateAvailability(tenant, cid, site string) {
	if a.availability == nil {
		return
	}
	m := a.availability
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, e := range m.entries {
		if e.target.tenant == tenant && (cid == "" || e.target.camera.ID == cid) && (site == "" || e.target.camera.SiteID == site) {
			if e.cancel != nil {
				e.cancel()
			}
			delete(m.entries, key)
		}
	}
}

type availabilityRow struct {
	row    interface{ Scan(...any) error }
	tenant *string
}

func (r availabilityRow) Scan(dest ...any) error {
	return r.row.Scan(append([]any{r.tenant}, dest...)...)
}

func (a *App) availabilityLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	slots := make(chan struct{}, 4)
	for {
		ctx, cancel := context.WithTimeout(a.runContext, 2*time.Second)
		rows, err := a.db.Query(ctx, strings.Replace(cameraSelect, "SELECT ", "SELECT c.tenant_id,", 1))
		targets := []availabilityTarget{}
		if err == nil {
			for rows.Next() {
				var tenant string
				c, e := scanCamera(availabilityRow{rows, &tenant})
				if e != nil {
					err = e
					break
				}
				targets = append(targets, availabilityTarget{tenant, c})
			}
			if e := rows.Err(); e != nil {
				err = e
			}
			rows.Close()
		}
		cancel()
		if err == nil {
			a.availability.reconcile(targets, time.Now())
			m := a.availability
			m.mu.Lock()
			due := []*availabilityEntry{}
			for _, e := range m.entries {
				if e.target.camera.EffectiveActive && !e.running && !time.Now().Before(e.next) {
					due = append(due, e)
				}
			}
			sort.Slice(due, func(i, j int) bool { return due[i].next.Before(due[j].next) })
			for _, e := range due {
				select {
				case slots <- struct{}{}:
				default:
					continue
				}
				run, stop := context.WithTimeout(a.runContext, 26*time.Second)
				e.cancel = stop
				e.running = true
				e.next = time.Now().Add(availabilityInterval)
				// Copy metadata before releasing the lock: reconciliation updates targets.
				target := e.target
				if !a.startBackground(func() {
					defer func() {
						<-slots
						stop()
						m.mu.Lock()
						e.running = false
						e.cancel = nil
						m.mu.Unlock()
					}()
					a.checkAvailability(run, e, target)
				}) {
					<-slots
					stop()
					e.running = false
					e.cancel = nil
				}
			}
			m.mu.Unlock()
		}
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) checkAvailability(ctx context.Context, e *availabilityEntry, target availabilityTarget) {
	c := target.camera
	configured, err := a.loadCameraSource(ctx, target.tenant, c.ID, c.SourceVersion)
	result := media.AvailabilityResult{}
	code := ""
	if err != nil {
		code = "vault_unavailable"
	} else {
		request := media.AvailabilityRequest{Tenant: target.tenant, Camera: c.ID, Source: c.SourceVersion, ConnectionRevision: c.ConnectionRevision}
		for _, p := range c.Profiles {
			if p.Enabled != nil && !*p.Enabled {
				continue
			}
			address, resolveErr := camera.Resolve(configured, p.ID)
			if resolveErr != nil {
				code = "invalid_configuration"
				break
			}
			path := "c_" + c.ID + "_" + c.SourceVersion + "_" + p.ID + "_r" + strconv.FormatInt(c.ConnectionRevision, 10)
			request.Profiles = append(request.Profiles, media.AvailabilityProfile{ID: p.ID, URL: address, Paths: []string{path + "_native", path + "_compatible", path + "_converted"}})
		}
		if len(request.Profiles) == 0 {
			code = "profiles_unavailable"
		}
		if code == "" && a.worker(ctx, "POST", "/availability", request, &result) != nil {
			code = "worker_unavailable"
		}
	}
	if ctx.Err() != nil {
		return
	}
	// Revalidate after I/O; an old connection's success must never update a new one.
	current, err := scanCamera(a.db.QueryRow(ctx, cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", target.tenant, c.ID))
	if err != nil || !current.EffectiveActive || availabilitySignature(current) != e.signature {
		return
	}
	a.availability.apply(e, result, code, time.Now().UTC())
}
func (a *App) attachAvailability(tenant string, c *Camera) {
	status := a.availability.snapshot(tenant, *c, time.Now())
	c.Availability = &status
}
