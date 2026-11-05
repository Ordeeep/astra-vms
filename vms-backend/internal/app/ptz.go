package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/ptz"
)

const ptzTTL = 2 * time.Second

func ptzRevision(c Camera) string {
	return c.SourceVersion + "/" + strconv.FormatInt(c.ConnectionRevision, 10) + "/" + c.SiteID
}
func ptzOwner(id identity, r *http.Request) string {
	return id.SessionHash + "/" + r.Header.Get("X-VMS-Context")
}

func (a *App) ptzLoop() {
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-a.runContext.Done():
			return
		case now := <-tick.C:
			a.ptz.Sweep(now)
		}
	}
}

func ptzProblem(w http.ResponseWriter, err error) {
	code := err.Error()
	status := 422
	switch code {
	case "ptz_superseded", "ptz_expired", "ptz_source_changed":
		status = 409
	case "ptz_unavailable", "camera_api_unavailable", "camera_api_timeout", "vault_unavailable":
		status = 503
	case "ptz_capacity":
		status = 429
	}
	problem(w, status, code)
}

func (a *App) controlPTZ(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	cid, token := r.PathValue("camera"), r.PathValue("command")
	key, owner := tenant+"/"+cid, ptzOwner(id, r)
	// Ownership permits cleanup even after PTZ/live grants were revoked. This
	// endpoint can only stop the command this session AND tab actually started.
	if r.Method == "DELETE" {
		if _, err := a.ptz.Stop(key, owner, token); err != nil {
			ptzProblem(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	var input struct {
		camera.PTZMove
		Home     bool   `json:"home"`
		Revision string `json:"revision"`
	}
	if r.Method == "POST" {
		if !body(w, r, &input) {
			return
		}
		if (!input.Home && !input.PTZMove.Valid()) || (input.Home && input.PTZMove != (camera.PTZMove{})) {
			problem(w, 422, "invalid_ptz_command")
			return
		}
	}
	tx, current, err := a.accessTransactionLock(r, id, true)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	fail := func(status int, code string) {
		if token != "" {
			a.ptz.Stop(key, owner, token)
		}
		problem(w, status, code)
	}
	c, err := scanCamera(tx.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2 FOR SHARE OF c,s,t,v", tenant, cid))
	if err != nil {
		fail(404, "camera_not_found")
		return
	}
	if !allowedWith(r.Context(), tx, current, tenant, "ptz.control", c.SiteID, c.ID) {
		fail(403, "forbidden")
		return
	}
	if !c.EffectiveActive {
		fail(409, "ptz_camera_inactive")
		return
	}
	revision := ptzRevision(c)
	if r.Method == "PUT" {
		if err = a.ptz.Renew(key, owner, token, revision); err != nil {
			ptzProblem(w, err)
			return
		}
		response(w, 200, map[string]any{"id": token, "expiresInMs": ptzTTL.Milliseconds()})
		return
	}
	if r.Method == "POST" && input.Revision != revision {
		problem(w, 409, "ptz_source_changed")
		return
	}
	source, err := a.loadCameraSourceFrom(r.Context(), tx, tenant, cid, c.SourceVersion)
	if err != nil {
		problem(w, 503, "vault_unavailable")
		return
	}
	driver, err := camera.OpenPTZ(source)
	if r.Method == "GET" {
		caps := camera.PTZCapabilities{}
		if err == nil {
			caps, err = driver.Capabilities(r.Context())
		}
		if err != nil && err.Error() != "ptz_unsupported" && err.Error() != "camera_api_unsupported" {
			ptzProblem(w, err)
			return
		}
		response(w, 200, struct {
			camera.PTZCapabilities
			Revision string `json:"revision"`
		}{caps, revision})
		return
	}
	if err != nil {
		ptzProblem(w, err)
		return
	}
	token, err = a.ptz.Start(r.Context(), ptz.Target{Key: key, Revision: revision, Driver: driver}, owner, input.PTZMove, input.Home)
	if err != nil {
		ptzProblem(w, err)
		return
	}
	details := map[string]any{"commandId": token, "pan": input.Pan, "tilt": input.Tilt, "zoom": input.Zoom, "home": input.Home}
	if err = audit(r.Context(), tx, current, tenant, "ptz.command", cid, details); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		a.ptz.Stop(key, owner, token)
		ptzProblem(w, errors.New("ptz_unavailable"))
		return
	}
	response(w, 201, map[string]any{"id": token, "expiresInMs": ptzTTL.Milliseconds()})
}

func (a *App) registerPTZ(mux *http.ServeMux) {
	base := " /api/v1/tenants/{tenant}/cameras/{camera}/ptz"
	mux.HandleFunc("GET"+base, a.protected(a.controlPTZ))
	mux.HandleFunc("POST"+base+"/commands", a.protected(a.controlPTZ))
	mux.HandleFunc("PUT"+base+"/commands/{command}", a.protected(a.controlPTZ))
	mux.HandleFunc("DELETE"+base+"/commands/{command}", a.protected(a.controlPTZ))
}
