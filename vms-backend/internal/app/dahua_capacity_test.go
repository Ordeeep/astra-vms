package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"vms.local/backend/internal/app"
)

func TestDahuaRediscoveryRemovesUnsupportedSavedProfileWithoutReplacingSource(t *testing.T) {
	var maximum atomic.Int32
	maximum.Store(3)
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("name") {
		case "MaxExtraStream":
			fmt.Fprintf(w, "table.MaxExtraStream=%d", maximum.Load())
		case "RTSP":
			fmt.Fprint(w, "table.RTSP.Port=554")
		case "Encode":
			fmt.Fprintln(w, "table.Encode[0].MainFormat[0].Video.Compression=H.265")
			for i := 0; i < 3; i++ {
				fmt.Fprintf(w, "table.Encode[0].ExtraFormat[%d].Video.Compression=H.265\ntable.Encode[0].ExtraFormat[%d].VideoEnable=true\n", i, i)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer device.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leases" {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			json.NewEncoder(w).Encode(map[string]any{"token": "capacity-test-lease", "expires": input["expires"]})
			return
		}
		w.WriteHeader(204)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "capacity-fixture" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Capacity lab", "timezone": "UTC"}, 201)["id"]
	u, _ := url.Parse(device.URL)
	port, _ := strconv.Atoi(u.Port())
	connection := map[string]any{"host": u.Hostname(), "protocol": "http", "httpPort": port, "username": "fixture", "password": "fixture-only"}
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Capacity camera", "siteId": site, "connectionType": "dahua", "connection": connection}, 201)
	if len(created["profiles"].([]any)) != 4 {
		t.Fatal("three-stream fixture was not established")
	}
	maximum.Store(2)
	delete(connection, "password")
	cameraURL := base + "/cameras/" + created["id"].(string)
	updated := b.call("PATCH", cameraURL+"/connection", map[string]any{"revision": created["revision"], "connectionType": "dahua", "connection": connection}, 200)
	if len(updated["profiles"].([]any)) != 3 || updated["sourceVersion"] != created["sourceVersion"] || updated["connection"].(map[string]any)["hasPassword"] != true {
		t.Fatal("rediscovery failed to retain origin and credentials while correcting profiles")
	}
	// Live reads the encrypted source, independently of the public profile list.
	rejected := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": created["id"], "profile": "sub3"}, 422)
	if rejected["code"] != "profile_unavailable" {
		t.Fatal("unsupported encrypted profile survived rediscovery")
	}
	live := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": created["id"], "profile": "sub3", "fallback": []string{"sub2", "main"}}, 201)
	if live["profile"] != "sub2" || live["fallback"] != true {
		t.Fatal("corrected source did not use the authorized fallback")
	}
}
