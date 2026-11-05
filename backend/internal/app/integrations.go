package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
	"vms.local/backend/internal/camera"
)

func (a *App) loadCameraSource(ctx context.Context, tenant, cid, source string) (camera.Source, error) {
	return a.loadCameraSourceFrom(ctx, a.db, tenant, cid, source)
}
func (a *App) loadCameraSourceFrom(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenant, cid, source string) (camera.Source, error) {
	var cipher []byte
	if e := q.QueryRow(ctx, "SELECT v.secrets FROM camera_sources v JOIN cameras c ON c.tenant_id=v.tenant_id AND c.id=v.camera_id WHERE v.tenant_id=$1 AND v.camera_id=$2 AND v.id=$3 AND c.deleted_at IS NULL", tenant, cid, source).Scan(&cipher); e != nil {
		return camera.Source{}, e
	}
	plain, e := a.unseal(cipher, tenant+"/"+cid+"/"+source)
	if e != nil {
		return camera.Source{}, e
	}
	var s camera.Source
	// Adopt legacy encrypted []Profile without rewriting history or credentials.
	if len(plain) > 0 && plain[0] == '[' {
		var p []Profile
		e = json.Unmarshal(plain, &p)
		s = camera.DecodeLegacy(p)
	} else {
		e = json.Unmarshal(plain, &s)
	}
	return s, e
}
func maintenanceSource(next, stored camera.Source) (camera.Source, error) {
	if next.Type == "" {
		next.Type = stored.Type
	}
	if next.Type != stored.Type {
		return next, errors.New("use_source_replacement")
	}
	// Public profile data excludes URLs. Empty values retain the saved address
	// of that profile when another field in the shared form changes.
	if next.Type == "rtsp" {
		next.Profiles = append([]Profile(nil), next.Profiles...)
		for i := range next.Profiles {
			if next.Profiles[i].URL != "" {
				continue
			}
			for _, p := range stored.Profiles {
				if p.ID == next.Profiles[i].ID {
					next.Profiles[i].URL = p.URL
					break
				}
			}
		}
	}
	if next.Connection != nil && next.Connection.Password == nil && stored.Connection != nil {
		c := *next.Connection
		c.Password = stored.Connection.Password
		next.Connection = &c
	}
	return camera.Normalize(next)
}

// Manufacturer profiles and transport ports are authoritative camera data.
// Caller-supplied copies are never used to build the saved video origin.
func sourceInput(s camera.Source) camera.Source {
	if camera.ManagedProfiles(s.Type) {
		s.Profiles = []Profile{}
		if s.Connection != nil {
			c := *s.Connection
			c.RTSPPort = 0
			c.ProfileStatus = "pending"
			s.Connection = &c
		}
	}
	return s
}
func sourceInputProblem(w http.ResponseWriter, e error) {
	status := 422
	if camera.Code(e) == "probe_capacity" {
		status = 429
	}
	problem(w, status, camera.Code(e))
}

// This runs before the mutation transaction. The caller revalidates resource
// authority and revision inside that transaction after network I/O completes.
func (a *App) prepareSource(ctx context.Context, s camera.Source, stored *camera.Source) (camera.Source, error) {
	if s.Type == "" && stored != nil {
		s.Type = stored.Type
	}
	s = sourceInput(s)
	var e error
	if stored != nil {
		s, e = maintenanceSource(s, *stored)
	} else {
		s, e = camera.Normalize(s)
	}
	if e != nil {
		return s, e
	}
	if !camera.ManagedProfiles(s.Type) {
		return s, nil
	}
	select {
	case a.probeSlots <- struct{}{}:
	default:
		return s, errors.New("probe_capacity")
	}
	defer func() { <-a.probeSlots }()
	s, _, _ = a.discoverSource(ctx, s, stored)
	if ctx.Err() != nil {
		return s, errors.New("camera_api_timeout")
	}
	return s, nil
}

func (a *App) discoverSource(ctx context.Context, s camera.Source, stored *camera.Source) (camera.Source, camera.Discovery, error) {
	// A failed fresh discovery can only reuse data of this same stored origin.
	if stored != nil && stored.Type == s.Type && stored.Connection != nil && stored.Connection.RTSPPort > 0 && len(stored.Profiles) > 0 {
		s.Profiles = stored.Profiles
		s.Connection.RTSPPort = stored.Connection.RTSPPort
		s.Connection.ProfileStatus = "known"
	}
	result, e := camera.Discover(ctx, s)
	if e == nil {
		s.Profiles = result.Profiles
		s.Connection.RTSPPort = result.RTSPPort
		s.Connection.ProfileStatus = "discovered"
	}
	return s, result, e
}

// Authorize the resource before any network call or use of a stored credential.
func (a *App) authorizeSourceRequest(w http.ResponseWriter, r *http.Request, id identity, tenant, site, cid string) (Camera, bool) {
	var c Camera
	if cid != "" {
		var e error
		c, e = scanCamera(a.db.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", tenant, cid))
		if e != nil {
			problem(w, 404, "camera_not_found")
			return c, false
		}
		site = c.SiteID
	} else {
		var exists bool
		if e := a.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=$2)", tenant, site).Scan(&exists); e != nil {
			dbFailure(w)
			return c, false
		}
		if !exists {
			problem(w, 404, "site_not_found")
			return c, false
		}
	}
	if !a.allowed(r.Context(), id, tenant, "inventory.manage", site, cid) {
		problem(w, 403, "forbidden")
		return c, false
	}
	return c, true
}
func (a *App) previewSource(ctx context.Context, tenant string, c Camera, s camera.Source) (camera.Source, error) {
	s = sourceInput(s)
	if c.ID != "" && s.Type == c.ConnectionType && s.Connection != nil && s.Connection.Password == nil {
		stored, e := a.loadCameraSource(ctx, tenant, c.ID, c.SourceVersion)
		if e != nil {
			return s, errors.New("vault_unavailable")
		}
		return maintenanceSource(s, stored)
	}
	return camera.Normalize(s)
}
func (a *App) sourceDiscovery(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		SiteID   string `json:"siteId"`
		CameraID string `json:"cameraId"`
		camera.Source
	}
	if !body(w, r, &in) {
		return
	}
	c, ok := a.authorizeSourceRequest(w, r, id, tenant, in.SiteID, in.CameraID)
	if !ok {
		return
	}
	s, e := a.previewSource(r.Context(), tenant, c, in.Source)
	if e != nil {
		problem(w, 422, camera.Code(e))
		return
	}
	select {
	case a.probeSlots <- struct{}{}:
	default:
		problem(w, 429, "probe_capacity")
		return
	}
	defer func() { <-a.probeSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	var stored *camera.Source
	if c.ID != "" && c.ConnectionType == s.Type {
		known, err := a.loadCameraSource(ctx, tenant, c.ID, c.SourceVersion)
		if err != nil {
			problem(w, 503, "vault_unavailable")
			return
		}
		stored = &known
	}
	var result camera.Discovery
	if camera.ManagedProfiles(s.Type) {
		s, result, e = a.discoverSource(ctx, s, stored)
	} else {
		result, e = camera.Discover(ctx, s)
	}
	api := map[string]string{"status": "ok"}
	if e != nil {
		api = map[string]string{"status": "error", "code": camera.Code(e)}
		result.Profiles = s.Profiles
	}
	response(w, 200, map[string]any{"api": api, "device": result.Device, "profiles": result.Profiles, "connection": camera.Public(s).Connection})
}
