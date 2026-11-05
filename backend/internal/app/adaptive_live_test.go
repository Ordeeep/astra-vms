package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"vms.local/backend/internal/app"
)

func TestAdaptiveLivePreparationAndConnectionMaintenance(t *testing.T) {
	var mu sync.Mutex
	var preparations []map[string]any
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sources":
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			mu.Lock()
			preparations = append(preparations, input)
			mu.Unlock()
			// Preparing a compatible source may take longer than a live lease.
			if input["forceTranscode"] == true {
				time.Sleep(1500 * time.Millisecond)
			}
			w.WriteHeader(204)
		case "/leases":
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			json.NewEncoder(w).Encode(map[string]any{"token": "test-lease", "expires": input["expires"]})
		default:
			w.Write([]byte("[]"))
		}
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "adaptive-test-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Adaptive lab", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Original codec camera", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera.test/original"}}}, 201)
	cid := camera["id"].(string)
	compatible := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": cid, "profile": "main", "codecs": []string{"h264"}}, 201)
	native := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": cid, "profile": "main", "codecs": []string{"h264", "h265"}}, 201)
	converted := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": cid, "profile": "main", "codecs": []string{"h264", "h265"}, "forceCompatibility": true}, 201)
	if compatible["delivery"] != "compatible" || native["delivery"] != "native" || converted["delivery"] != "compatible" {
		t.Fatal("delivery strategy was not negotiated")
	}
	// This renewal would fail if the lease began before conversion preparation.
	b.call("PUT", base+"/live-sessions/"+converted["id"].(string), map[string]any{}, 200)
	mu.Lock()
	if len(preparations) != 3 || preparations[0]["compatible"] != true || preparations[1]["compatible"] != false || preparations[2]["forceTranscode"] != true {
		t.Fatal("worker did not receive the requested compatibility policy")
	}
	if preparations[0]["path"] == preparations[1]["path"] || preparations[1]["path"] == preparations[2]["path"] {
		t.Fatal("delivery variants must not overwrite each other")
	}
	mu.Unlock()
	updated := b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": camera["revision"], "connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera.test/new-address"}}}, 200)
	if updated["sourceVersion"] != camera["sourceVersion"] {
		t.Fatal("connection maintenance changed the recording origin")
	}
	b.call("PUT", base+"/live-sessions/"+converted["id"].(string), map[string]any{}, 410)
	b.call("POST", base+"/live-sessions", map[string]any{"cameraId": cid, "profile": "main"}, 201)
	mu.Lock()
	defer mu.Unlock()
	if preparations[3]["path"] == preparations[0]["path"] || preparations[3]["url"] != "rtsp://camera.test/new-address" {
		t.Fatal("new connection reused stale media configuration")
	}
}
