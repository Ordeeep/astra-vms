package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"vms.local/backend/internal/camera"
)

type Site struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Address      string          `json:"address"`
	Timezone     string          `json:"timezone"`
	Active       bool            `json:"active"`
	Revision     int64           `json:"revision"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}
type Profile = camera.Profile
type Camera struct {
	Availability       *CameraAvailability `json:"availability,omitempty"`
	ID                 string              `json:"id"`
	SiteID             string              `json:"siteId"`
	Name               string              `json:"name"`
	Active             bool                `json:"active"`
	EffectiveActive    bool                `json:"effectiveActive"`
	Revision           int64               `json:"revision"`
	SourceVersion      string              `json:"sourceVersion"`
	ConnectionType     string              `json:"connectionType"`
	Connection         *camera.Connection  `json:"connection,omitempty"`
	ConnectionRevision int64               `json:"connectionRevision"`
	Profiles           []Profile           `json:"profiles"`
	Capabilities       map[string]bool     `json:"capabilities"`
}

var profileID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func validName(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 200 }
func publicProfiles(profiles []Profile) ([]Profile, bool) {
	if len(profiles) == 0 || len(profiles) > 16 {
		return nil, false
	}
	seen := map[string]bool{}
	public := make([]Profile, 0, len(profiles))
	for _, p := range profiles {
		u, err := url.Parse(p.URL)
		if !profileID.MatchString(p.ID) || seen[p.ID] || !validName(p.Name) || len(p.URL) > 4096 || err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" || u.Fragment != "" {
			return nil, false
		}
		seen[p.ID] = true
		public = append(public, Profile{ID: p.ID, Name: p.Name})
	}
	return public, seen["main"]
}
func dbFailure(w http.ResponseWriter) { problem(w, 503, "database_unavailable") }
func (a *App) sites(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if r.Method == "GET" {
		grants, err := a.grants(r.Context(), id.User.ID, tenant)
		if err != nil {
			dbFailure(w)
			return
		}
		rows, err := a.db.Query(r.Context(), "SELECT id,name,address,timezone,active,revision FROM sites WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 201", tenant, r.URL.Query().Get("cursor"))
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		items := []Site{}
		cursor := ""
		count := 0
		for rows.Next() {
			var s Site
			if rows.Scan(&s.ID, &s.Name, &s.Address, &s.Timezone, &s.Active, &s.Revision) != nil {
				dbFailure(w)
				return
			}
			count++
			if count > 200 {
				break
			}
			cursor = s.ID
			visible := id.User.GlobalAdmin
			s.Capabilities = map[string]bool{"inventory.manage": id.User.GlobalAdmin || permits(grants, tenant, "inventory.manage", s.ID, "")}
			for _, g := range grants {
				if (g.Scope == "tenant" && g.ResourceID == tenant) || (g.Scope == "site" && g.ResourceID == s.ID) {
					visible = true
				}
			}
			if visible {
				items = append(items, s)
			}
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		if count <= 200 {
			cursor = ""
		}
		response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
		return
	}
	var in Site
	if !body(w, r, &in) {
		return
	}
	if !validName(in.Name) || len(in.Address) > 2000 {
		problem(w, 422, "invalid_site")
		return
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		problem(w, 422, "invalid_timezone")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	sid := r.PathValue("site")
	if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", sid, "") {
		problem(w, 403, "forbidden")
		return
	}
	status := 200
	if r.Method == "POST" {
		in.ID = newID()
		in.Active = true
		in.Revision = 1
		status = 201
		_, err = tx.Exec(r.Context(), "INSERT INTO sites(id,tenant_id,name,address,timezone) VALUES($1,$2,$3,$4,$5)", in.ID, tenant, in.Name, in.Address, in.Timezone)
	} else {
		result, e := tx.Exec(r.Context(), "UPDATE sites SET name=$1,address=$2,timezone=$3,active=$4,revision=revision+1 WHERE tenant_id=$5 AND id=$6 AND revision=$7", in.Name, in.Address, in.Timezone, in.Active, tenant, sid, in.Revision)
		err = e
		if err == nil && result.RowsAffected() != 1 {
			problem(w, 412, "revision_conflict")
			return
		}
		in.ID = sid
		in.Revision++
	}
	if err != nil {
		dbFailure(w)
		return
	}
	if audit(r.Context(), tx, current, tenant, "site.saved", in.ID, in) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	if !in.Active {
		a.invalidateAvailability(tenant, "", in.ID)
	}
	response(w, status, in)
}

const cameraSelect = `SELECT c.id,c.site_id,c.name,c.active,(c.active AND s.active AND t.active),c.revision,c.source_version,v.profiles,v.connection_type,v.connection,v.connection_revision FROM cameras c JOIN sites s ON s.id=c.site_id AND s.tenant_id=c.tenant_id JOIN tenants t ON t.id=c.tenant_id JOIN camera_sources v ON v.id=c.source_version AND v.camera_id=c.id AND v.tenant_id=c.tenant_id AND c.deleted_at IS NULL`

func scanCamera(row interface{ Scan(...any) error }) (Camera, error) {
	var c Camera
	var raw, connection []byte
	err := row.Scan(&c.ID, &c.SiteID, &c.Name, &c.Active, &c.EffectiveActive, &c.Revision, &c.SourceVersion, &raw, &c.ConnectionType, &connection, &c.ConnectionRevision)
	if err == nil {
		err = json.Unmarshal(raw, &c.Profiles)
		if err == nil && string(connection) != "null" {
			err = json.Unmarshal(connection, &c.Connection)
		}
	}
	return c, err
}
func cameraCapabilities(c *Camera, grants []Grant, tenant string, admin bool) bool {
	c.Capabilities = map[string]bool{}
	visible := false
	for _, action := range actions {
		allowed := permits(grants, tenant, action, c.SiteID, c.ID)
		if action == "ptz.control" {
			allowed = allowed && permits(grants, tenant, "live", c.SiteID, c.ID)
		}
		if action == "export" {
			allowed = allowed && permits(grants, tenant, "playback", c.SiteID, c.ID)
		}
		if admin && (action == "inventory.manage" || action == "camera.delete" || action == "recording.manage" || action == "permissions.manage" || action == "audit.view") {
			allowed = true
		}
		c.Capabilities[action] = allowed
		visible = visible || allowed
	}
	if !c.Capabilities["inventory.manage"] {
		c.Connection = nil
	}
	return visible
}
func (a *App) cameraResponse(w http.ResponseWriter, r *http.Request, id identity, tenant, cid string, status int) {
	c, err := scanCamera(a.db.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", tenant, cid))
	if err != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	if !cameraCapabilities(&c, grants, tenant, id.User.GlobalAdmin) {
		problem(w, 404, "camera_not_found")
		return
	}
	a.attachAvailability(tenant, &c)
	response(w, status, c)
}
func (a *App) cameras(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if r.Method == "GET" {
		if cid := r.PathValue("camera"); cid != "" {
			a.cameraResponse(w, r, id, tenant, cid, 200)
			return
		}
		grants, err := a.grants(r.Context(), id.User.ID, tenant)
		if err != nil {
			dbFailure(w)
			return
		}
		rows, err := a.db.Query(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id>$2 ORDER BY c.id LIMIT 201", tenant, r.URL.Query().Get("cursor"))
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		items := []Camera{}
		cursor := ""
		count := 0
		for rows.Next() {
			c, err := scanCamera(rows)
			if err != nil {
				dbFailure(w)
				return
			}
			count++
			if count > 200 {
				break
			}
			cursor = c.ID
			if cameraCapabilities(&c, grants, tenant, id.User.GlobalAdmin) {
				a.attachAvailability(tenant, &c)
				items = append(items, c)
			}
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		if count <= 200 {
			cursor = ""
		}
		response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
		return
	}
	var in struct {
		Name   string `json:"name"`
		SiteID string `json:"siteId"`
		camera.Source
		Active   bool  `json:"active"`
		Revision int64 `json:"revision"`
	}
	if !body(w, r, &in) {
		return
	}
	if !validName(in.Name) {
		problem(w, 422, "invalid_camera")
		return
	}
	var configured camera.Source
	if r.Method == "POST" {
		var e error
		if _, allowed := a.authorizeSourceRequest(w, r, id, tenant, in.SiteID, ""); !allowed {
			return
		}
		configured, e = a.prepareSource(r.Context(), in.Source, nil)
		if e != nil {
			sourceInputProblem(w, e)
			return
		}
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	cid := r.PathValue("camera")
	site := in.SiteID
	var oldActive bool
	if r.Method == "PATCH" {
		if tx.QueryRow(r.Context(), "SELECT site_id,active FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL", tenant, cid).Scan(&site, &oldActive) != nil {
			problem(w, 404, "camera_not_found")
			return
		}
	}
	if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", site, cid) {
		problem(w, 403, "forbidden")
		return
	}
	var exists bool
	if tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=$2)", tenant, site).Scan(&exists) != nil || !exists {
		problem(w, 404, "site_not_found")
		return
	}
	status := 200
	if r.Method == "POST" {
		cid = newID()
		source := newID()
		status = 201
		raw, _ := json.Marshal(configured)
		secret, e := a.seal(raw, tenant+"/"+cid+"/"+source)
		if e != nil {
			problem(w, 503, "vault_unavailable")
			return
		}
		safe := camera.Public(configured)
		rawPublic, _ := json.Marshal(safe.Profiles)
		rawConnection, _ := json.Marshal(safe.Connection)
		_, err = tx.Exec(r.Context(), "INSERT INTO cameras(id,tenant_id,site_id,name,source_version) VALUES($1,$2,$3,$4,$5)", cid, tenant, site, in.Name, source)
		if err == nil {
			_, err = tx.Exec(r.Context(), "INSERT INTO camera_sources(id,tenant_id,camera_id,profiles,secrets,connection_type,connection) VALUES($1,$2,$3,$4,$5,$6,$7)", source, tenant, cid, rawPublic, secret, configured.Type, rawConnection)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), "INSERT INTO camera_locations(tenant_id,camera_id,site_id) VALUES($1,$2,$3)", tenant, cid, site)
		}
	} else {
		if in.SiteID != "" || len(in.Profiles) > 0 || in.Connection != nil || in.Type != "" {
			problem(w, 422, "use_explicit_camera_change")
			return
		}
		result, e := tx.Exec(r.Context(), "UPDATE cameras SET name=$1,active=$2,revision=revision+1 WHERE tenant_id=$3 AND id=$4 AND revision=$5 AND deleted_at IS NULL", in.Name, in.Active, tenant, cid, in.Revision)
		err = e
		if err == nil && result.RowsAffected() != 1 {
			problem(w, 412, "revision_conflict")
			return
		}
	}
	if err != nil {
		dbFailure(w)
		return
	}
	if audit(r.Context(), tx, current, tenant, "camera.saved", cid, map[string]any{"siteId": site, "name": in.Name}) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	if r.Method == "PATCH" && oldActive != in.Active {
		a.invalidateAvailability(tenant, cid, "")
	}
	a.cameraResponse(w, r, current, tenant, cid, status)
}
func (a *App) cameraChange(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	cid := r.PathValue("camera")
	var in struct {
		SiteID string `json:"siteId"`
		camera.Source
		Revision int64 `json:"revision"`
	}
	if !body(w, r, &in) {
		return
	}
	var configured camera.Source
	if !strings.HasSuffix(r.URL.Path, "site-changes") {
		existing, allowed := a.authorizeSourceRequest(w, r, id, tenant, "", cid)
		if !allowed {
			return
		}
		if existing.Revision != in.Revision {
			problem(w, 412, "revision_conflict")
			return
		}
		var stored *camera.Source
		if r.Method == "PATCH" {
			known, e := a.loadCameraSource(r.Context(), tenant, cid, existing.SourceVersion)
			if e != nil {
				problem(w, 503, "vault_unavailable")
				return
			}
			stored = &known
		}
		var e error
		configured, e = a.prepareSource(r.Context(), in.Source, stored)
		if e != nil {
			sourceInputProblem(w, e)
			return
		}
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	var oldSite string
	var rev int64
	if tx.QueryRow(r.Context(), "SELECT site_id,revision FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", tenant, cid).Scan(&oldSite, &rev) != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", oldSite, cid) {
		problem(w, 403, "forbidden")
		return
	}
	if rev != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	var details any
	action := "camera.source_changed"
	if strings.HasSuffix(r.URL.Path, "site-changes") {
		var exists bool
		if tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=$2)", tenant, in.SiteID).Scan(&exists) != nil || !exists {
			problem(w, 404, "site_not_found")
			return
		}
		if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", in.SiteID, "") {
			problem(w, 403, "forbidden")
			return
		}
		_, err = tx.Exec(r.Context(), "UPDATE cameras SET site_id=$1,revision=revision+1 WHERE tenant_id=$2 AND id=$3", in.SiteID, tenant, cid)
		if err == nil {
			_, err = tx.Exec(r.Context(), "INSERT INTO camera_locations(tenant_id,camera_id,site_id) VALUES($1,$2,$3)", tenant, cid, in.SiteID)
		}
		action = "camera.site_changed"
		details = map[string]string{"previousSite": oldSite, "siteId": in.SiteID}
	} else {
		maintenance := r.Method == "PATCH"

		source := newID()
		if maintenance {
			if tx.QueryRow(r.Context(), "SELECT source_version FROM cameras WHERE tenant_id=$1 AND id=$2", tenant, cid).Scan(&source) != nil {
				problem(w, 404, "camera_not_found")
				return
			}
		}
		raw, _ := json.Marshal(configured)
		secret, e := a.seal(raw, tenant+"/"+cid+"/"+source)
		if e != nil {
			problem(w, 503, "vault_unavailable")
			return
		}
		safe := camera.Public(configured)
		rawPublic, _ := json.Marshal(safe.Profiles)
		rawConnection, _ := json.Marshal(safe.Connection)
		if maintenance {
			_, err = tx.Exec(r.Context(), "UPDATE camera_sources SET profiles=$1,secrets=$2,connection_type=$3,connection=$4,connection_revision=connection_revision+1 WHERE tenant_id=$5 AND camera_id=$6 AND id=$7", rawPublic, secret, configured.Type, rawConnection, tenant, cid, source)
			action = "camera.connection_updated"
		} else {
			_, err = tx.Exec(r.Context(), "INSERT INTO camera_sources(id,tenant_id,camera_id,profiles,secrets,connection_type,connection) VALUES($1,$2,$3,$4,$5,$6,$7)", source, tenant, cid, rawPublic, secret, configured.Type, rawConnection)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), "UPDATE cameras SET source_version=$1,revision=revision+1 WHERE tenant_id=$2 AND id=$3 AND revision=$4", source, tenant, cid, in.Revision)
		}
		details = map[string]string{"sourceVersion": source, "connectionType": configured.Type}

	}
	if err != nil {
		dbFailure(w)
		return
	}
	if audit(r.Context(), tx, current, tenant, action, cid, details) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	a.invalidateAvailability(tenant, cid, "")
	a.cameraResponse(w, r, current, tenant, cid, 200)
}
func (a *App) sourceVersions(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	cid := r.PathValue("camera")
	var site string
	if a.db.QueryRow(r.Context(), "SELECT site_id FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL", tenant, cid).Scan(&site) != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !a.allowed(r.Context(), id, tenant, "inventory.manage", site, cid) {
		problem(w, 403, "forbidden")
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT id,profiles,created_at FROM camera_sources WHERE tenant_id=$1 AND camera_id=$2 ORDER BY created_at DESC LIMIT 200", tenant, cid)
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var source string
		var raw json.RawMessage
		var created time.Time
		if rows.Scan(&source, &raw, &created) != nil {
			dbFailure(w)
			return
		}
		items = append(items, map[string]any{"id": source, "profiles": raw, "createdAt": created})
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"items": items})
}
func (a *App) registerInventory(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST"} {
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/sites", a.protected(a.sites))
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/cameras", a.protected(a.cameras))
	}
	mux.HandleFunc("PATCH /api/v1/tenants/{tenant}/sites/{site}", a.protected(a.sites))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/cameras/{camera}", a.protected(a.cameras))
	mux.HandleFunc("PATCH /api/v1/tenants/{tenant}/cameras/{camera}", a.protected(a.cameras))
	mux.HandleFunc("DELETE /api/v1/tenants/{tenant}/cameras/{camera}", a.protected(a.deleteCamera))
	mux.HandleFunc("PATCH /api/v1/tenants/{tenant}/cameras/{camera}/configuration", a.protected(a.cameraConfiguration))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/cameras/{camera}/source-changes", a.protected(a.cameraChange))
	mux.HandleFunc("PATCH /api/v1/tenants/{tenant}/cameras/{camera}/connection", a.protected(a.cameraChange))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/source-discovery", a.protected(a.sourceDiscovery))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/cameras/{camera}/site-changes", a.protected(a.cameraChange))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/cameras/{camera}/source-versions", a.protected(a.sourceVersions))
}
