package app

import (
	"encoding/json"
	"net/http"
	"vms.local/backend/internal/camera"
)

// Save the shared camera form atomically while retaining the distinct source
// maintenance, equipment replacement and location histories.
func (a *App) cameraConfiguration(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Name     string         `json:"name"`
		SiteID   string         `json:"siteId"`
		Active   bool           `json:"active"`
		Revision int64          `json:"revision"`
		Source   *camera.Source `json:"source"`
	}
	if !body(w, r, &in) {
		return
	}
	if !validName(in.Name) || in.SiteID == "" {
		problem(w, 422, "invalid_camera")
		return
	}
	cid := r.PathValue("camera")
	existing, allowed := a.authorizeSourceRequest(w, r, id, tenant, "", cid)
	if !allowed {
		return
	}
	if existing.Revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	var configured camera.Source
	// Ordinary edits retain the origin. Switching integrations starts a new
	// source version automatically without requiring a separate operator mode.
	replaceSource := in.Source != nil && in.Source.Type != "" && in.Source.Type != existing.ConnectionType
	if in.Source != nil {
		var stored *camera.Source
		if !replaceSource {
			known, err := a.loadCameraSource(r.Context(), tenant, cid, existing.SourceVersion)
			if err != nil {
				problem(w, 503, "vault_unavailable")
				return
			}
			stored = &known
		}
		var err error
		configured, err = a.prepareSource(r.Context(), *in.Source, stored)
		if err != nil {
			sourceInputProblem(w, err)
			return
		}
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	var oldSite, source string
	var revision int64
	if tx.QueryRow(r.Context(), "SELECT site_id,source_version,revision FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", tenant, cid).Scan(&oldSite, &source, &revision) != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", oldSite, cid) {
		problem(w, 403, "forbidden")
		return
	}
	if revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	if oldSite != in.SiteID {
		var exists bool
		if tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=$2)", tenant, in.SiteID).Scan(&exists) != nil || !exists {
			problem(w, 404, "site_not_found")
			return
		}
		if !allowedWith(r.Context(), tx, current, tenant, "inventory.manage", in.SiteID, "") {
			problem(w, 403, "forbidden")
			return
		}
	}
	if in.Source != nil {
		if replaceSource {
			source = newID()
		}
		raw, _ := json.Marshal(configured)
		secret, e := a.seal(raw, tenant+"/"+cid+"/"+source)
		if e != nil {
			problem(w, 503, "vault_unavailable")
			return
		}
		safe := camera.Public(configured)
		profiles, _ := json.Marshal(safe.Profiles)
		connection, _ := json.Marshal(safe.Connection)
		if replaceSource {
			_, err = tx.Exec(r.Context(), "INSERT INTO camera_sources(id,tenant_id,camera_id,profiles,secrets,connection_type,connection) VALUES($1,$2,$3,$4,$5,$6,$7)", source, tenant, cid, profiles, secret, configured.Type, connection)
		} else {
			_, err = tx.Exec(r.Context(), "UPDATE camera_sources SET profiles=$1,secrets=$2,connection_type=$3,connection=$4,connection_revision=connection_revision+1 WHERE tenant_id=$5 AND camera_id=$6 AND id=$7", profiles, secret, configured.Type, connection, tenant, cid, source)
		}
		if err != nil {
			dbFailure(w)
			return
		}
	}
	_, err = tx.Exec(r.Context(), "UPDATE cameras SET name=$1,active=$2,site_id=$3,source_version=$4,revision=revision+1 WHERE tenant_id=$5 AND id=$6", in.Name, in.Active, in.SiteID, source, tenant, cid)
	if err != nil {
		dbFailure(w)
		return
	}
	if oldSite != in.SiteID {
		if _, err = tx.Exec(r.Context(), "INSERT INTO camera_locations(tenant_id,camera_id,site_id) VALUES($1,$2,$3)", tenant, cid, in.SiteID); err != nil {
			dbFailure(w)
			return
		}
		if audit(r.Context(), tx, current, tenant, "camera.site_changed", cid, map[string]string{"previousSite": oldSite, "siteId": in.SiteID}) != nil {
			dbFailure(w)
			return
		}
	}
	if in.Source != nil {
		action := "camera.connection_updated"
		if replaceSource {
			action = "camera.source_changed"
		}
		if audit(r.Context(), tx, current, tenant, action, cid, map[string]string{"sourceVersion": source, "connectionType": configured.Type}) != nil {
			dbFailure(w)
			return
		}
	}
	if audit(r.Context(), tx, current, tenant, "camera.saved", cid, map[string]any{"siteId": in.SiteID, "name": in.Name}) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	if in.Source != nil || in.Active != existing.Active || oldSite != in.SiteID {
		a.invalidateAvailability(tenant, cid, "")
	}
	a.cameraResponse(w, r, current, tenant, cid, 200)
}
