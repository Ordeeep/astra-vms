package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/media"
)

type workerError struct{ Status int }

func (e workerError) Error() string { return "media command rejected" }
func (a *App) worker(ctx context.Context, method, path string, input, output any) error {
	if a.opts.WorkerURL == "" || a.opts.WorkerSecret == "" {
		return errors.New("media worker unavailable")
	}
	raw, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, method, a.opts.WorkerURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.opts.WorkerSecret)
	req.Header.Set("Content-Type", "application/json")
	timeout := 12 * time.Second
	if path == "/sources" {
		timeout = 20 * time.Second
	}
	if path == "/availability" {
		timeout = 26 * time.Second
	}
	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return errors.New("media worker unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return workerError{res.StatusCode}
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(output)
	}
	return nil
}
func (a *App) cameraSource(ctx context.Context, tenant, cid, source, profile string) (string, error) {
	configured, err := a.loadCameraSource(ctx, tenant, cid, source)
	if err != nil {
		return "", err
	}
	return camera.Resolve(configured, profile)
}

// sourceTest probes an unsaved source without persisting its URL or credentials.
func (a *App) sourceTest(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		URL      string `json:"url"`
		SiteID   string `json:"siteId"`
		CameraID string `json:"cameraId"`
		Profile  string `json:"profile"`
		camera.Source
	}
	if !body(w, r, &in) {
		return
	}
	c, ok := a.authorizeSourceRequest(w, r, id, tenant, in.SiteID, in.CameraID)
	if !ok {
		return
	}
	select {
	case a.probeSlots <- struct{}{}:
	default:
		problem(w, 429, "probe_capacity")
		return
	}
	defer func() { <-a.probeSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
	defer cancel()
	source := in.URL
	if in.Type != "" || in.Connection != nil || len(in.Profiles) > 0 {
		s, e := a.previewSource(r.Context(), tenant, c, in.Source)
		if e != nil {
			problem(w, 422, camera.Code(e))
			return
		}
		if camera.ManagedProfiles(s.Type) {
			var stored *camera.Source
			if c.ID != "" && c.ConnectionType == s.Type {
				known, e := a.loadCameraSource(ctx, tenant, c.ID, c.SourceVersion)
				if e != nil {
					problem(w, 503, "vault_unavailable")
					return
				}
				stored = &known
			}
			s, _, _ = a.discoverSource(ctx, s, stored)
		}
		if in.Profile == "" {
			in.Profile = "main"
		}
		source, e = camera.Resolve(s, in.Profile)
		if e != nil {
			problem(w, 422, camera.Code(e))
			return
		}
	} else if _, valid := publicProfiles([]Profile{{ID: "main", Name: "Principal", URL: source}}); !valid {
		problem(w, 422, "invalid_rtsp_url")
		return
	}
	var result media.ProbeResult
	if a.worker(ctx, "POST", "/probe", map[string]string{"url": source}, &result) != nil {
		problem(w, 422, "source_probe_failed")
		return
	}
	response(w, 200, map[string]any{"video": result})
}

func (a *App) profileTest(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	camera := r.PathValue("camera")
	var in struct {
		Profile string `json:"profile"`
	}
	if !body(w, r, &in) {
		return
	}
	c, err := scanCamera(a.db.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", tenant, camera))
	if err != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !a.allowed(r.Context(), id, tenant, "inventory.manage", c.SiteID, c.ID) {
		problem(w, 403, "forbidden")
		return
	}
	source, err := a.cameraSource(r.Context(), tenant, c.ID, c.SourceVersion, in.Profile)
	if err != nil {
		problem(w, 422, "profile_unavailable")
		return
	}
	job := newID()
	select {
	case a.probeSlots <- struct{}{}:
	default:
		problem(w, 429, "probe_capacity")
		return
	}
	if _, err = a.db.Exec(r.Context(), "INSERT INTO jobs(id,tenant_id,actor_id,camera_id,kind,status) VALUES($1,$2,$3,$4,'profile-test','running')", job, tenant, id.User.ID, c.ID); err != nil {
		<-a.probeSlots
		dbFailure(w)
		return
	}
	if !a.startBackground(func() {
		defer func() { <-a.probeSlots }()
		ctx, cancel := context.WithTimeout(a.runContext, 15*time.Second)
		defer cancel()
		var result media.ProbeResult
		status := "completed"
		data := map[string]any{}
		if a.worker(ctx, "POST", "/probe", map[string]string{"url": source}, &result) != nil {
			status = "failed"
			data["code"] = "source_probe_failed"
		} else {
			data["video"] = result
			data["sourceVersion"] = c.SourceVersion
			data["profile"] = in.Profile
		}
		raw, _ := json.Marshal(data)
		finish, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		a.db.Exec(finish, "UPDATE jobs SET status=$1,result=$2,finished_at=now() WHERE id=$3", status, raw, job)
	}) {
		<-a.probeSlots
		a.db.Exec(r.Context(), "UPDATE jobs SET status='failed',result='{\"code\":\"service_stopping\"}',finished_at=now() WHERE id=$1", job)
		problem(w, 503, "service_stopping")
		return
	}
	response(w, 202, map[string]string{"id": job, "status": "running"})
}
func (a *App) jobStatus(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var camera, status, kind, site string
	var result json.RawMessage
	var created time.Time
	err := a.db.QueryRow(r.Context(), `SELECT j.camera_id,j.kind,j.status,j.result,j.created_at,c.site_id FROM jobs j JOIN cameras c ON c.id=j.camera_id AND c.tenant_id=j.tenant_id WHERE j.tenant_id=$1 AND j.id=$2`, tenant, r.PathValue("job")).Scan(&camera, &kind, &status, &result, &created, &site)
	if err != nil {
		problem(w, 404, "job_not_found")
		return
	}
	if !a.allowed(r.Context(), id, tenant, "inventory.manage", site, camera) {
		problem(w, 403, "forbidden")
		return
	}
	response(w, 200, map[string]any{"id": r.PathValue("job"), "kind": kind, "status": status, "result": result, "createdAt": created})
}

type liveSession struct {
	ID, Camera, Source, Profile, Token, Path, Native string
	Expires                                          time.Time
}

func (a *App) loadLive(r *http.Request, id identity, tenant string) (liveSession, error) {
	var s liveSession
	err := a.db.QueryRow(r.Context(), "SELECT id,camera_id,source_version,profile_id,worker_token,media_path,native_url,expires FROM live_sessions WHERE id=$1 AND tenant_id=$2 AND context_id=$3 AND session_hash=$4 AND expires>now()", r.PathValue("live"), tenant, r.Header.Get("X-VMS-Context"), id.SessionHash).Scan(&s.ID, &s.Camera, &s.Source, &s.Profile, &s.Token, &s.Path, &s.Native, &s.Expires)
	if err != nil {
		return s, err
	}
	c, err := scanCamera(a.db.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", tenant, s.Camera))
	if err != nil || !c.EffectiveActive || c.SourceVersion != s.Source || !liveConnectionCurrent(c, s) || !a.allowed(r.Context(), id, tenant, "live", c.SiteID, c.ID) {
		return s, errors.New("live authorization lost")
	}
	return s, nil
}
func (a *App) createLive(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		CameraID           string   `json:"cameraId"`
		Profile            string   `json:"profile"`
		Fallback           []string `json:"fallback"`
		Codecs             []string `json:"codecs"`
		ForceCompatibility bool     `json:"forceCompatibility"`
	}
	if !body(w, r, &in) {
		return
	}
	if len(in.Fallback) > 16 || len(in.Codecs) > 16 {
		problem(w, 422, "invalid_fallback")
		return
	}
	c, err := scanCamera(a.db.QueryRow(r.Context(), cameraSelect+" WHERE c.tenant_id=$1 AND c.id=$2", tenant, in.CameraID))
	if err != nil {
		problem(w, 404, "camera_not_found")
		return
	}
	if !a.allowed(r.Context(), id, tenant, "live", c.SiteID, c.ID) {
		problem(w, 403, "forbidden")
		return
	}
	if !c.EffectiveActive {
		problem(w, 409, "camera_inactive")
		return
	}
	chosen, source := "", ""
	for _, candidate := range append([]string{in.Profile}, in.Fallback...) {
		value, e := a.cameraSource(r.Context(), tenant, c.ID, c.SourceVersion, candidate)
		if e == nil {
			chosen = candidate
			source = value
			break
		}
	}
	if chosen == "" {
		problem(w, 422, "profile_unavailable")
		return
	}
	mode := deliveryMode(in.Codecs, in.ForceCompatibility)
	path := "c_" + c.ID + "_" + c.SourceVersion + "_" + chosen + "_r" + strconv.FormatInt(c.ConnectionRevision, 10) + "_" + mode
	if a.worker(r.Context(), "POST", "/sources", map[string]any{"path": path, "url": source, "compatible": mode != "native", "forceTranscode": in.ForceCompatibility}, nil) != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	// Preparing a shared compatible stream may take seconds. The short lease
	// starts afterwards, so codec adaptation does not consume its lifetime.
	deadline := time.Now().Add(1400 * time.Millisecond)
	var lease media.Lease
	if a.worker(r.Context(), "POST", "/leases", map[string]any{"tenant": tenant, "context": r.Header.Get("X-VMS-Context"), "path": path, "expires": deadline}, &lease) != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	sid := newID()
	_, err = a.db.Exec(r.Context(), "WITH inserted AS (INSERT INTO live_sessions(id,tenant_id,context_id,session_hash,camera_id,source_version,profile_id,worker_token,media_path,expires) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id) INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) SELECT $11,$2,'live.started',$5 FROM inserted", sid, tenant, r.Header.Get("X-VMS-Context"), id.SessionHash, c.ID, c.SourceVersion, chosen, lease.Token, path, lease.Expires, id.User.ID)
	if err != nil {
		a.worker(r.Context(), "DELETE", "/leases/"+lease.Token, nil, nil)
		dbFailure(w)
		return
	}
	// Revalidate after the worker round-trip, including camera state and source.
	r.SetPathValue("live", sid)
	if _, err = a.loadLive(r, id, tenant); err != nil || !contextValid(r.Context(), a.db, r, id) {
		a.worker(r.Context(), "DELETE", "/leases/"+lease.Token, nil, nil)
		problem(w, 403, "forbidden")
		return
	}
	delivery := "native"
	if mode != "native" {
		delivery = "compatible"
	}
	response(w, 201, map[string]any{"id": sid, "profile": chosen, "fallback": chosen != in.Profile, "delivery": delivery, "expires": lease.Expires, "signaling": "/api/v1/tenants/" + tenant + "/live-sessions/" + sid + "/whep"})
}

func liveConnectionCurrent(c Camera, s liveSession) bool {
	base := "c_" + c.ID + "_" + c.SourceVersion + "_" + s.Profile
	// Legacy sessions expire quickly but may still exist during an update.
	if c.ConnectionRevision <= 1 && s.Path == base {
		return true
	}
	for _, mode := range []string{"native", "compatible", "converted"} {
		if s.Path == base+"_r"+strconv.FormatInt(c.ConnectionRevision, 10)+"_"+mode {
			return true
		}
	}
	return false
}
func (a *App) liveControl(w http.ResponseWriter, r *http.Request, id identity) {
	expires := time.Now().Add(1400 * time.Millisecond)
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if r.Method == "DELETE" {
		var token string
		err := a.db.QueryRow(r.Context(), "DELETE FROM live_sessions WHERE id=$1 AND tenant_id=$2 AND context_id=$3 AND session_hash=$4 RETURNING worker_token", r.PathValue("live"), tenant, r.Header.Get("X-VMS-Context"), id.SessionHash).Scan(&token)
		if err == nil {
			a.worker(r.Context(), "DELETE", "/leases/"+token, nil, nil)
		}
		w.WriteHeader(204)
		return
	}
	s, err := a.loadLive(r, id, tenant)
	if err != nil {
		problem(w, 410, "live_session_ended")
		return
	}
	if a.worker(r.Context(), "PUT", "/leases/"+s.Token, map[string]any{"expires": expires}, nil) != nil {
		problem(w, 410, "live_session_ended")
		return
	}
	if _, err = a.db.Exec(r.Context(), "UPDATE live_sessions SET expires=$1 WHERE id=$2", expires, s.ID); err != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"expires": expires})
}
func (a *App) signaling(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	s, err := a.loadLive(r, id, tenant)
	if err != nil {
		problem(w, 410, "live_session_ended")
		return
	}
	target := a.opts.WHEPURL + "/" + s.Path + "/whep"
	if r.Method != "POST" {
		if s.Native == "" {
			problem(w, 409, "signaling_not_open")
			return
		}
		target = s.Native
	} else if s.Native != "" {
		problem(w, 409, "signaling_already_open")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		problem(w, 400, "invalid_sdp")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(raw))
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("If-Match", "*")
	res, err := (&http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	defer res.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		problem(w, 502, "media_unavailable")
		return
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 && r.Method == "POST" {
		base, _ := url.Parse(target)
		loc, err := base.Parse(res.Header.Get("Location"))
		if err != nil || loc.Scheme != base.Scheme || loc.Host != base.Host || !strings.HasPrefix(loc.Path, "/"+s.Path+"/whep/") {
			problem(w, 502, "invalid_media_location")
			return
		}
		if a.worker(r.Context(), "POST", "/leases/"+s.Token+"/established", nil, nil) != nil {
			a.worker(r.Context(), "DELETE", "/leases/"+s.Token, nil, nil)
			problem(w, 410, "live_session_ended")
			return
		}
		result, err := a.db.Exec(r.Context(), "UPDATE live_sessions SET native_url=$1 WHERE id=$2 AND native_url=''", loc.String(), s.ID)
		if err != nil || result.RowsAffected() != 1 {
			a.worker(r.Context(), "DELETE", "/leases/"+s.Token, nil, nil)
			problem(w, 409, "signaling_conflict")
			return
		}
		if _, err = a.loadLive(r, id, tenant); err != nil {
			a.worker(r.Context(), "DELETE", "/leases/"+s.Token, nil, nil)
			problem(w, 410, "live_session_ended")
			return
		}
		w.Header().Set("Location", r.URL.Path)
		w.Header().Set("ETag", res.Header.Get("ETag"))
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(res.StatusCode)
	w.Write(answer)
}
func (a *App) registerMedia(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/source-tests", a.protected(a.sourceTest))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/cameras/{camera}/profile-tests", a.protected(a.profileTest))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/jobs/{job}", a.protected(a.jobStatus))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/live-sessions", a.protected(a.createLive))
	for _, m := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(m+" /api/v1/tenants/{tenant}/live-sessions/{live}", a.protected(a.liveControl))
	}
	for _, m := range []string{"POST", "PATCH", "DELETE"} {
		mux.HandleFunc(m+" /api/v1/tenants/{tenant}/live-sessions/{live}/whep", a.protected(a.signaling))
	}
}
