package app_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/app"
)

func TestDahuaInventoryMaintenancePermissionsAndIndependentTests(t *testing.T) {
	var apiCalls atomic.Int32
	cameraAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera",nonce="fixture",qop="auth"`)
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("name") == "RTSP" {
			fmt.Fprint(w, "table.RTSP.Port=8554")
			return
		}
		if r.URL.Query().Get("name") == "MaxExtraStream" {
			fmt.Fprint(w, "table.MaxExtraStream=1")
			return
		}
		if r.URL.Query().Get("name") == "Encode" {
			fmt.Fprint(w, "table.Encode[0].MainFormat[0].Video.Compression=H.265\ntable.Encode[0].ExtraFormat[0].Video.Compression=H.264")
			return
		}
		w.WriteHeader(404)
	}))
	defer cameraAPI.Close()
	var originsMu sync.Mutex
	origins := []string{}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			fmt.Fprint(w, "[]")
			return
		}
		var v map[string]string
		json.NewDecoder(r.Body).Decode(&v)
		originsMu.Lock()
		origins = append(origins, v["url"])
		originsMu.Unlock()
		if strings.Contains(v["url"], "unavailable") {
			http.Error(w, "private", 422)
			return
		}
		fmt.Fprint(w, `{"codec":"h265","width":1920,"height":1080,"fps":25}`)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "integration-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Dahua Lab", "timezone": "UTC"}, 201)["id"].(string)
	u, _ := url.Parse(cameraAPI.URL)
	port, _ := strconv.Atoi(u.Port())
	connection := map[string]any{"host": u.Hostname(), "protocol": "http", "httpPort": port, "rtspPort": 9999, "username": "camera-user", "password": "reserved:@/?&password"}
	profiles := []map[string]any{{"id": "main", "name": "Forged", "codec": "mjpeg"}, {"id": "sub99", "name": "Fake substream"}}
	created := b.call("POST", base+"/cameras", map[string]any{"siteId": site, "name": "Dahua", "connectionType": "dahua", "connection": connection, "profiles": profiles}, 201)
	cid := created["id"].(string)
	source := created["sourceVersion"].(string)
	if apiCalls.Load() == 0 || created["connectionType"] != "dahua" || created["connectionRevision"] != float64(1) {
		t.Fatal("save did not discover manufacturer configuration")
	}
	savedProfiles := created["profiles"].([]any)
	if len(savedProfiles) != 2 || savedProfiles[0].(map[string]any)["codec"] != "h.265" || savedProfiles[1].(map[string]any)["id"] != "sub1" {
		t.Fatal("client profiles bypassed manufacturer discovery")
	}
	if created["connection"].(map[string]any)["profileStatus"] != "discovered" || created["connection"].(map[string]any)["rtspPort"] != nil {
		t.Fatal("private transport exposed or status lost")
	}
	offlineConnection := map[string]any{"host": "127.0.0.1", "protocol": "http", "httpPort": 1, "rtspPort": 9999, "username": "offline", "password": "offline-password"}
	offline := b.call("POST", base+"/cameras", map[string]any{"siteId": site, "name": "Offline", "connectionType": "dahua", "connection": offlineConnection, "profiles": profiles}, 201)
	if len(offline["profiles"].([]any)) != 0 || offline["connection"].(map[string]any)["profileStatus"] != "pending" {
		t.Fatal("offline source invented profiles")
	}
	failedPending := b.call("POST", base+"/source-tests", map[string]any{"cameraId": offline["id"], "connectionType": "dahua", "connection": offlineConnection, "profiles": profiles, "profile": "main"}, 422)
	if failedPending["code"] != "camera_discovery_required" {
		t.Fatal("pending source pretended known transport")
	}

	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "reserved:") || strings.Contains(string(raw), "rtsp://") || strings.Contains(string(raw), `"password":`) {
		t.Fatal("credential leaked")
	}
	delete(connection, "password")
	discovery := b.call("POST", base+"/source-discovery", map[string]any{"cameraId": cid, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 200)
	if discovery["api"].(map[string]any)["status"] != "ok" || len(discovery["profiles"].([]any)) != 2 {
		t.Fatal(discovery)
	}
	probe := map[string]any{"cameraId": cid, "connectionType": "dahua", "connection": connection, "profiles": profiles, "profile": "sub1"}
	video := b.call("POST", base+"/source-tests", probe, 200)
	if video["video"].(map[string]any)["codec"] != "h265" {
		t.Fatal(video)
	}
	originsMu.Lock()
	first := origins[0]
	originsMu.Unlock()
	parsed, _ := url.Parse(first)
	password, _ := parsed.User.Password()
	if password != "reserved:@/?&password" || parsed.Query().Get("subtype") != "1" || parsed.Port() != "8554" {
		t.Fatal("stored password/stream was not preserved")
	}
	probe["profile"] = "sub99"
	if rejected := b.call("POST", base+"/source-tests", probe, 422); rejected["code"] != "profile_unavailable" {
		t.Fatal("fake profile reached video test")
	}
	probe["profile"] = "sub1"
	connection["rtspPort"] = 9999
	updated := b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": 1, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 200)
	if updated["sourceVersion"] != source || updated["connectionRevision"] != float64(2) || updated["revision"] != float64(2) || updated["connection"].(map[string]any)["hasPassword"] != true {
		t.Fatal("maintenance changed origin or failed revision")
	}
	b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": 1, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 412)
	b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": 2, "connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://camera/video"}}}, 422)
	// HTTP can fail while the same known RTSP profiles remain testable.
	connection["httpPort"] = 1
	discovery = b.call("POST", base+"/source-discovery", map[string]any{"cameraId": cid, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 200)
	if discovery["api"].(map[string]any)["status"] != "error" {
		t.Fatal("API outage hidden")
	}
	updated = b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": 2, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 200)
	if updated["sourceVersion"] != source || len(updated["profiles"].([]any)) != 2 || updated["connection"].(map[string]any)["profileStatus"] != "known" {
		t.Fatal("offline maintenance lost private known configuration")
	}
	b.call("POST", base+"/source-tests", probe, 200)
	originsMu.Lock()
	last := origins[len(origins)-1]
	originsMu.Unlock()
	parsed, _ = url.Parse(last)
	password, _ = parsed.User.Password()
	if parsed.Port() != "8554" || password != "reserved:@/?&password" {
		t.Fatal("connection maintenance not applied")
	}
	// API success does not hide unavailable RTSP.
	connection["httpPort"] = port
	connection["host"] = "unavailable"
	b.call("POST", base+"/source-tests", probe, 422)
	connection["host"] = u.Hostname()
	before := apiCalls.Load()
	b.call("POST", base+"/source-discovery", map[string]any{"cameraId": "foreign", "connectionType": "dahua", "connection": connection, "profiles": profiles}, 404)
	if apiCalls.Load() != before {
		t.Fatal("foreign resource reached camera")
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Viewer", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	viewer := b.call("POST", "/admin/operators", map[string]any{"username": "dahua-viewer", "password": "viewer-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+viewer, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.call("POST", "/auth/logout", nil, 204)
	b.login("dahua-viewer", "viewer-password")
	b.selectTenant(tenant)
	visible := b.call("GET", base+"/cameras/"+cid, nil, 200)
	if visible["connection"] != nil {
		t.Fatal("viewer received device connection details")
	}
	b.call("POST", base+"/source-discovery", map[string]any{"cameraId": cid, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 403)
	b.call("POST", base+"/source-tests", probe, 403)
	b.call("PATCH", base+"/cameras/"+cid+"/connection", map[string]any{"revision": 2, "connectionType": "dahua", "connection": connection, "profiles": profiles}, 403)
	if apiCalls.Load() != before {
		t.Fatal("unauthorized request reached camera")
	}
	b.call("POST", "/auth/logout", nil, 204)
	b.login("admin", "admin")
	b.selectTenant(tenant)
	replacement := b.call("POST", base+"/cameras/"+cid+"/source-changes", map[string]any{"revision": 3, "connectionType": "dahua", "connection": offlineConnection, "profiles": profiles}, 200)
	if replacement["sourceVersion"] == source || replacement["connectionType"] != "dahua" || replacement["connectionRevision"] != float64(1) || len(replacement["profiles"].([]any)) != 0 || replacement["connection"].(map[string]any)["profileStatus"] != "pending" {
		t.Fatal("new offline origin inherited former connection data")
	}

	if len(b.call("GET", base+"/cameras/"+cid+"/source-versions", nil, 200)["items"].([]any)) != 2 {
		t.Fatal("source history was lost")
	}
}

func TestLegacyRTSPSecretEnvelopeRemainsProbeable(t *testing.T) {
	observed := make(chan string, 1)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			fmt.Fprint(w, "[]")
			return
		}
		var v map[string]string
		json.NewDecoder(r.Body).Decode(&v)
		observed <- v["url"]
		fmt.Fprint(w, `{"codec":"h264","width":640,"height":360,"fps":15}`)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "legacy-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Legacy", "timezone": "UTC"}, 201)["id"].(string)
	profiles := []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://legacy:secret@camera/video"}}
	created := b.call("POST", base+"/cameras", map[string]any{"siteId": site, "name": "Legacy", "profiles": profiles}, 201)
	// Simulate a pre-migration encrypted []Profile, using this isolated lab key.
	key, e := os.ReadFile(filepath.Join(b.options.DataDir, "camera-key.bin"))
	if e != nil {
		t.Fatal(e)
	}
	block, _ := aes.NewCipher(key)
	vault, _ := cipher.NewGCM(block)
	nonce := make([]byte, vault.NonceSize())
	rand.Read(nonce)
	plain, _ := json.Marshal(profiles)
	cid := created["id"].(string)
	source := created["sourceVersion"].(string)
	secret := vault.Seal(nonce, nonce, plain, []byte(tenant+"/"+cid+"/"+source))
	db, e := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close(context.Background())
	table := pgx.Identifier{b.options.Schema, "camera_sources"}.Sanitize()
	if _, e = db.Exec(context.Background(), "UPDATE "+table+" SET secrets=$1 WHERE id=$2", secret, source); e != nil {
		t.Fatal(e)
	}
	b.call("POST", base+"/cameras/"+cid+"/profile-tests", map[string]string{"profile": "main"}, 202)
	select {
	case address := <-observed:
		if address != "rtsp://legacy:secret@camera/video" {
			t.Fatal("legacy source failed resolution")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("legacy source was not probed")
	}
}
