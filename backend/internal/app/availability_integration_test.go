package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

func TestCameraAvailabilityAPIReusesCacheAndInvalidatesEdits(t *testing.T) {
	var probes atomic.Int32
	oldStarted := make(chan struct{}, 1)
	oldCanceled := make(chan struct{}, 1)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/availability" {
			probes.Add(1)
			var in media.AvailabilityRequest
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				w.WriteHeader(400)
				return
			}
			for _, p := range in.Profiles {
				if strings.HasSuffix(p.URL, "/old") {
					select {
					case oldStarted <- struct{}{}:
					default:
					}
					<-r.Context().Done()
					select {
					case oldCanceled <- struct{}{}:
					default:
					}
					return
				}
			}
			at := time.Now().UTC()
			json.NewEncoder(w).Encode(media.AvailabilityResult{Profiles: []media.VideoObservation{{Profile: "main", Available: true, Complete: true, LastVideoAt: &at, CheckedAt: at}}})
			return
		}
		if r.URL.Path == "/archive/config" {
			w.Write([]byte("{\"configured\":false}"))
			return
		}
		w.Write([]byte("[]"))
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "test" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Matriz", "timezone": "UTC"}, 201)["id"].(string)
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Entrada", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://fixture:private@camera/old"}}}, 201)
	path := base + "/cameras/" + created["id"].(string)
	select {
	case <-oldStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("continuous monitor did not start without a viewer")
	}
	updated := b.call("PATCH", path+"/configuration", map[string]any{"name": "Entrada", "siteId": site, "active": true, "revision": 1, "source": map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://fixture:private@camera/current"}}}}, 200)
	if updated["availability"].(map[string]any)["state"] != "unknown" {
		t.Fatal("edited connection must invalidate immediately")
	}
	select {
	case <-oldCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("obsolete worker request was not canceled")
	}
	var current map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current = b.call("GET", path, nil, 200)
		if current["availability"].(map[string]any)["state"] == "online" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if current["availability"].(map[string]any)["state"] != "online" {
		t.Fatal("new connection was not confirmed")
	}
	before := probes.Load()
	for i := 0; i < 5; i++ {
		b.call("GET", base+"/cameras", nil, 200)
	}
	if probes.Load() != before {
		t.Fatal("inventory reads must not trigger probes")
	}
	renamed := b.call("PATCH", path, map[string]any{"name": "Renomeada", "active": true, "revision": 2}, 200)
	if renamed["availability"].(map[string]any)["state"] != "online" {
		t.Fatal("rename invalidated healthy camera")
	}
	disabled := b.call("PATCH", path, map[string]any{"name": "Renomeada", "active": false, "revision": 3}, 200)
	if disabled["availability"].(map[string]any)["state"] != "suspended" {
		t.Fatal("disabled camera must suspend immediately")
	}
	enabled := b.call("PATCH", path, map[string]any{"name": "Renomeada", "active": true, "revision": 4}, 200)
	if enabled["availability"].(map[string]any)["state"] != "unknown" {
		t.Fatal("reactivation needs fresh evidence")
	}
	raw, _ := json.Marshal(current)
	if strings.Contains(string(raw), "rtsp://") || strings.Contains(string(raw), "private") {
		t.Fatal("availability exposed private connection")
	}
	b.call("DELETE", path, map[string]int{"revision": 5}, 202)
	b.call("GET", path, nil, 404)
	// The normal fixture cleanup closes the app and drops only its isolated schema.
}
