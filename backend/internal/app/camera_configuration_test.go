package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

func TestCameraConfigurationAtomicSaveAndSourceHistory(t *testing.T) {
	observed := make(chan string, 4)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			fmt.Fprint(w, "[]")
			return
		}
		var input map[string]string
		json.NewDecoder(r.Body).Decode(&input)
		observed <- input["url"]
		fmt.Fprint(w, `{"codec":"h264","width":640,"height":360,"fps":15}`)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "fixture-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	siteA := b.call("POST", base+"/sites", map[string]string{"name": "A", "timezone": "UTC"}, 201)["id"].(string)
	siteB := b.call("POST", base+"/sites", map[string]string{"name": "B", "timezone": "UTC"}, 201)["id"].(string)
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Before", "siteId": siteA, "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://fixture:secret@camera/main"}, {"id": "sub1", "name": "Sub", "url": "rtsp://fixture:secret@camera/sub"}}}, 201)
	cid := created["id"].(string)
	path := base + "/cameras/" + cid
	input := map[string]any{"name": "After", "siteId": siteB, "active": true, "revision": 1, "source": map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://camera/new"}, {"id": "sub1", "name": "Sub", "url": ""}}}}
	updated := b.call("PATCH", path+"/configuration", input, 200)
	if updated["name"] != "After" || updated["siteId"] != siteB || updated["revision"] != float64(2) || updated["sourceVersion"] != created["sourceVersion"] || updated["connectionRevision"] != float64(2) {
		t.Fatal("combined maintenance did not preserve origin and save all fields")
	}
	raw, _ := json.Marshal(updated)
	if strings.Contains(string(raw), "rtsp://") || strings.Contains(string(raw), "secret") {
		t.Fatal("private source returned")
	}
	b.call("POST", path+"/profile-tests", map[string]string{"profile": "sub1"}, 202)
	select {
	case address := <-observed:
		if address != "rtsp://fixture:secret@camera/sub" {
			t.Fatal("blank sibling address was not preserved")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("saved substream was not probed")
	}
	// A stale revision or invalid destination must not modify metadata or source.
	b.call("PATCH", path+"/configuration", input, 412)
	input["revision"] = 2
	input["siteId"] = "missing-site"
	input["name"] = "Must not save"
	b.call("PATCH", path+"/configuration", input, 404)
	current := b.call("GET", path, nil, 200)
	if current["name"] != "After" || current["revision"] != float64(2) || current["connectionRevision"] != float64(2) {
		t.Fatal("failed save partially committed")
	}
	// Metadata alone must work without resubmitting private connection fields.
	input = map[string]any{"name": "Metadata", "siteId": siteA, "active": false, "revision": 2}
	current = b.call("PATCH", path+"/configuration", input, 200)
	if current["active"] != false || current["connectionRevision"] != float64(2) || current["sourceVersion"] != created["sourceVersion"] {
		t.Fatal("metadata save changed connection")
	}
	// Profiles can be added or removed directly, retaining the saved URLs.
	input["revision"] = 3
	input["source"] = map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Main"}, {"id": "sub2", "name": "New substream", "url": "rtsp://camera/sub2"}}}
	current = b.call("PATCH", path+"/configuration", input, 200)
	if current["sourceVersion"] != created["sourceVersion"] || len(current["profiles"].([]any)) != 2 || current["connectionRevision"] != float64(3) {
		t.Fatal("direct profile editing changed the origin")
	}
	// Integration changes automatically version the source, without a mode flag.
	input["revision"] = 4
	connection := map[string]any{"host": "127.0.0.1", "protocol": "http", "httpPort": 1, "username": "fixture"}
	input["source"] = map[string]any{"connectionType": "dahua", "connection": connection}
	b.call("PATCH", path+"/configuration", input, 422)
	connection["password"] = "fixture-password"
	current = b.call("PATCH", path+"/configuration", input, 200)
	if current["sourceVersion"] == created["sourceVersion"] || current["revision"] != float64(5) || current["connectionType"] != "dahua" {
		t.Fatal("integration change reused the previous origin")
	}
	versions := b.call("GET", path+"/source-versions", nil, 200)["items"].([]any)
	if len(versions) != 2 {
		t.Fatal("source history was lost")
	}
}

func TestCameraConfigurationDestinationPermissions(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	siteA := b.call("POST", base+"/sites", map[string]string{"name": "A", "timezone": "UTC"}, 201)["id"].(string)
	siteB := b.call("POST", base+"/sites", map[string]string{"name": "B", "timezone": "UTC"}, 201)["id"].(string)
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Before", "siteId": siteA, "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://camera/main"}}}, 201)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "manager", "password": "fixture-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "A only", "grants": []app.Grant{{Action: "inventory.manage", Scope: "site", ResourceID: siteA}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.login("manager", "fixture-password")
	b.selectTenant(tenant)
	path := base + "/cameras/" + created["id"].(string)
	input := map[string]any{"name": "Must not save", "active": true, "siteId": siteB, "revision": 1}
	b.call("PATCH", path+"/configuration", input, 403)
	current := b.call("GET", path, nil, 200)
	if current["name"] != "Before" || current["revision"] != float64(1) {
		t.Fatal("unauthorized destination partially saved metadata")
	}
	input["siteId"] = siteA
	b.call("PATCH", path+"/configuration", input, 200)
}

func TestCameraConfigurationDahuaKeepsSavedPassword(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Lab", "timezone": "UTC"}, 201)["id"].(string)
	connection := map[string]any{"host": "127.0.0.1", "protocol": "http", "httpPort": 1, "username": "fixture", "password": "fixture-password"}
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Dahua", "siteId": site, "connectionType": "dahua", "connection": connection}, 201)
	delete(connection, "password")
	connection["username"] = "updated-user"
	input := map[string]any{"name": "Updated Dahua", "siteId": site, "active": true, "revision": 1, "source": map[string]any{"connectionType": "dahua", "connection": connection}}
	path := base + "/cameras/" + created["id"].(string) + "/configuration"
	updated := b.call("PATCH", path, input, 200)
	if updated["sourceVersion"] != created["sourceVersion"] || updated["connection"].(map[string]any)["hasPassword"] != true || updated["connection"].(map[string]any)["username"] != "updated-user" {
		t.Fatal("shared form lost the saved credentials")
	}
	input["revision"] = 2
	input["source"] = map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Main"}}}
	b.call("PATCH", path, input, 422)
	input["source"] = map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://camera/new"}}}
	updated = b.call("PATCH", path, input, 200)
	if updated["sourceVersion"] == created["sourceVersion"] || updated["connectionType"] != "rtsp" {
		t.Fatal("direct integration change did not save")
	}
}
