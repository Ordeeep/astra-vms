package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"vms.local/backend/internal/app"
)

func TestHikvisionInventoryDiscoveryMaintenanceAndPermissions(t *testing.T) {
	var offline atomic.Bool
	var calls atomic.Int32
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" {
			t.Error("device write")
		}
		if offline.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="fixture",nonce="n",qop="auth"`)
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/ISAPI/Streaming/channels":
			fmt.Fprint(w, `<StreamingChannelList><StreamingChannel><id>101</id><Video><videoCodecType>H.265</videoCodecType><maxFrameRate>2500</maxFrameRate></Video></StreamingChannel><StreamingChannel><id>102</id><Video><videoCodecType>H.264</videoCodecType><maxFrameRate>1250</maxFrameRate></Video></StreamingChannel></StreamingChannelList>`)
		case "/ISAPI/Security/adminAccesses":
			fmt.Fprint(w, `<AdminAccessProtocolList><AdminAccessProtocol><protocol>RTSP</protocol><portNo>8554</portNo></AdminAccessProtocol></AdminAccessProtocolList>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer device.Close()
	observed := make(chan string, 4)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			fmt.Fprint(w, "[]")
			return
		}
		var input map[string]string
		json.NewDecoder(r.Body).Decode(&input)
		observed <- input["url"]
		fmt.Fprint(w, `{"codec":"h264","width":640,"height":360,"fps":12.5}`)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "hikvision-test-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "ISAPI lab", "timezone": "UTC"}, 201)["id"].(string)
	u, _ := url.Parse(device.URL)
	port, _ := strconv.Atoi(u.Port())
	connection := map[string]any{"host": u.Hostname(), "protocol": "http", "httpPort": port, "username": "fixture", "password": "reserved:@/?&password", "rtspPort": 9999}
	input := map[string]any{"siteId": site, "name": "Hikvision fixture", "connectionType": "hikvision", "connection": connection, "profiles": []map[string]string{{"id": "main", "name": "Invented"}, {"id": "sub99", "name": "Invented"}}}
	created := b.call("POST", base+"/cameras", input, 201)
	cid := created["id"].(string)
	path := base + "/cameras/" + cid
	profiles := created["profiles"].([]any)
	if created["connectionType"] != "hikvision" || len(profiles) != 2 || profiles[1].(map[string]any)["id"] != "sub1" || created["connection"].(map[string]any)["profileStatus"] != "discovered" {
		t.Fatal("manufacturer discovery was not authoritative")
	}
	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "reserved:") || strings.Contains(string(raw), "password\"") || strings.Contains(string(raw), "rtspPort") {
		t.Fatal("public camera leaked private access data")
	}
	delete(connection, "password")
	probe := map[string]any{"cameraId": cid, "connectionType": "hikvision", "connection": connection, "profile": "sub1"}
	b.call("POST", base+"/source-tests", probe, 200)
	origin, _ := url.Parse(<-observed)
	password, _ := origin.User.Password()
	if origin.Path != "/ISAPI/Streaming/channels/102" || origin.Port() != "8554" || password != "reserved:@/?&password" {
		t.Fatal("saved credentials or discovered origin lost")
	}
	// Maintenance must preserve history even when only the HTTP API has failed.
	offline.Store(true)
	updated := b.call("PATCH", path+"/configuration", map[string]any{"name": "Updated", "siteId": site, "active": true, "revision": created["revision"], "source": map[string]any{"connectionType": "hikvision", "connection": connection}}, 200)
	if updated["sourceVersion"] != created["sourceVersion"] || updated["connectionRevision"] != float64(2) || updated["connection"].(map[string]any)["profileStatus"] != "known" || updated["connection"].(map[string]any)["hasPassword"] != true || len(updated["profiles"].([]any)) != 2 {
		t.Fatal("maintenance fragmented history or discarded known access")
	}
	discoveryInput := map[string]any{"cameraId": cid, "connectionType": "hikvision", "connection": connection}
	discovery := b.call("POST", base+"/source-discovery", discoveryInput, 200)
	if discovery["api"].(map[string]any)["status"] != "error" || len(discovery["profiles"].([]any)) != 2 {
		t.Fatal("API failure hid known profiles")
	}
	b.call("POST", base+"/source-tests", probe, 200)
	if next := <-observed; !strings.Contains(next, "/ISAPI/Streaming/channels/102") {
		t.Fatal("API outage prevented known video access")
	}
	connection["password"] = "fixture-only"
	pending := b.call("POST", base+"/cameras", input, 201)
	if pending["connection"].(map[string]any)["profileStatus"] != "pending" || len(pending["profiles"].([]any)) != 0 {
		t.Fatal("new offline source invented profiles")
	}
	probe["cameraId"] = pending["id"]
	failed := b.call("POST", base+"/source-tests", probe, 422)
	if failed["code"] != "camera_discovery_required" {
		t.Fatal("pending source reached video worker")
	}
	discoveryInput["cameraId"] = "foreign"
	before := calls.Load()
	b.call("POST", base+"/source-discovery", discoveryInput, 404)
	if calls.Load() != before {
		t.Fatal("foreign camera reached device")
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Viewer", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	viewer := b.call("POST", "/admin/operators", map[string]any{"username": "hikvision-viewer", "password": "viewer-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+viewer, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.call("POST", "/auth/logout", nil, 204)
	b.login("hikvision-viewer", "viewer-password")
	b.selectTenant(tenant)
	probe["cameraId"] = cid
	discoveryInput["cameraId"] = cid
	b.call("POST", base+"/source-discovery", discoveryInput, 403)
	b.call("POST", base+"/source-tests", probe, 403)
	visible := b.call("GET", path, nil, 200)
	if calls.Load() != before || visible["connection"] != nil {
		t.Fatal("viewer obtained camera access")
	}
}
