package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"vms.local/backend/internal/app"
)

func TestUnsavedSourceProbePermissionsValidationAndNoCameraCreation(t *testing.T) {
	var calls atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer probe-test-worker" {
			t.Error("missing worker authentication")
		}
		var input map[string]string
		json.NewDecoder(r.Body).Decode(&input)
		if input["url"] == "rtsp://unavailable/video" {
			http.Error(w, "private-worker-details", 422)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"codec":"h264","width":640,"height":360,"fps":15}`))
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "probe-test-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Probe lab", "timezone": "UTC"}, 201)["id"].(string)
	before := b.call("GET", base+"/cameras", nil, 200)["items"].([]any)
	result := b.call("POST", base+"/source-tests", map[string]string{"siteId": site, "url": "rtsp://user:test-password@camera/video"}, 200)
	if result["video"].(map[string]any)["codec"] != "h264" {
		t.Fatal("missing probe result")
	}
	b.call("POST", base+"/source-tests", map[string]string{"siteId": site, "url": "https://camera/video"}, 422)
	b.call("POST", base+"/source-tests", map[string]string{"siteId": "foreign", "url": "rtsp://camera/video"}, 404)
	failed := b.call("POST", base+"/source-tests", map[string]string{"siteId": site, "url": "rtsp://unavailable/video"}, 422)
	if failed["code"] != "source_probe_failed" {
		t.Fatal("worker failure was not sanitized")
	}
	after := b.call("GET", base+"/cameras", nil, 200)["items"].([]any)
	if len(before) != len(after) {
		t.Fatal("probe created a camera")
	}
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Existing camera", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera/original"}}}, 201)
	b.call("POST", base+"/source-tests", map[string]string{"cameraId": camera["id"].(string), "url": "rtsp://camera/replacement"}, 200)
	current := b.call("GET", base+"/cameras/"+camera["id"].(string), nil, 200)
	if current["sourceVersion"] != camera["sourceVersion"] || current["revision"] != camera["revision"] {
		t.Fatal("probe changed saved source")
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Viewer", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "probe-viewer", "password": "viewer-test-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.call("POST", "/auth/logout", nil, 204)
	b.login("probe-viewer", "viewer-test-password")
	b.selectTenant(tenant)
	b.call("POST", base+"/source-tests", map[string]string{"siteId": site, "url": "rtsp://camera/video"}, 403)
	b.call("POST", base+"/source-tests", map[string]string{"cameraId": camera["id"].(string), "url": "rtsp://camera/video"}, 403)
	if calls.Load() != 3 {
		t.Fatal("invalid or unauthorized probe reached worker")
	}
}
