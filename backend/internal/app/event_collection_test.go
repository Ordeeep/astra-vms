package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/camerafixture"
	"vms.local/backend/internal/eventcollector"
)

func TestEventCollectorEndToEnd(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef0123456789"
	store, err := eventcollector.Open(t.TempDir(), secret, 2<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker, err := eventcollector.Start(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(worker.Handler(secret))
	device := httptest.NewServer(&camerafixture.Device{Interval: time.Hour})
	defer device.Close()
	defer func() { cancel(); worker.Close(); collector.Close() }()
	b := newTestAPI(t, func(o *app.Options) { o.EventWorkerURL = collector.URL; o.WorkerSecret = secret })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Portaria", "timezone": "UTC"}, 201)["id"].(string)
	u, _ := url.Parse(device.URL)
	port, _ := strconv.Atoi(u.Port())
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Piloto", "siteId": site, "connectionType": "hikvision", "connection": map[string]any{"host": u.Hostname(), "protocol": "http", "httpPort": port, "username": "test", "password": "fixture"}}, 201)
	config := base + "/cameras/" + created["id"].(string) + "/event-collection"
	inventories := b.call("GET", config+"/rules", nil, 200)
	if inv := inventories["inventory"].(map[string]any)["line_crossing"].(map[string]any); inv["complete"] != true || len(inv["items"].([]any)) != 2 {
		t.Fatal("incomplete discovery", inv)
	}
	settings := map[string]any{"enabled": true, "revision": 0, "rules": []map[string]string{{"kind": "line_crossing", "id": "7", "label": "Portão"}}}
	b.call("PUT", config, settings, 200)
	b.call("PUT", config, settings, 412)
	deadline := time.Now().Add(25 * time.Second)
	for {
		items := b.call("GET", base+"/analytic-events", nil, 200)["items"].([]any)
		faults := b.call("GET", base+"/analytic-events/failures", nil, 200)["items"].([]any)
		if len(items) == 4 && len(faults) == 1 {
			observed := map[string]bool{}
			for _, row := range items {
				item := row.(map[string]any)
				if item["simulated"] != false || item["siteId"] != site {
					t.Fatal("unexpected receipt", item)
				}
				observed[fmt.Sprint(item["ruleId"], "/", item["state"])] = true
			}
			for _, key := range []string{"8/active", "8/inactive", "1/active", "1/inactive"} {
				if !observed[key] {
					t.Fatal("missing unsupervised event", key, items)
				}
			}
			if faults[0].(map[string]any)["code"] != "rule_disabled" {
				t.Fatal("unexpected supervision", faults)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery timeout, events=%v faults=%v", items, faults)
		}
		time.Sleep(150 * time.Millisecond)
	}
	health := b.call("GET", "/health/event-collector", nil, 200)
	if health["connected"] != true {
		t.Fatal(health)
	}
	settings["enabled"] = false
	settings["revision"] = 1
	b.call("PUT", config, settings, 200)
	closed := b.call("GET", base+"/analytic-events/failures?closed=1", nil, 200)["items"].([]any)
	found := false
	for _, v := range closed {
		f := v.(map[string]any)
		if f["code"] == "rule_disabled" && f["endReason"] == "suspended" {
			found = true
		}
	}
	if !found {
		t.Fatal("operator suspension missing")
	}
	raw := fmt.Sprint(b.call("GET", config, nil, 200))
	if strings.Contains(raw, "fixture") {
		t.Fatal("credential disclosure")
	}
	b.close()
}

func TestEventCollectorDelayedDelivery(t *testing.T) {
	var offline atomic.Bool
	var acks atomic.Int32
	var mu sync.Mutex
	var pending []eventcollector.Record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "offline", 503)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/batch":
			json.NewEncoder(w).Encode(eventcollector.Batch{Items: pending})
		case "/ack":
			if acks.Add(1) == 1 {
				http.Error(w, "retry", 503)
				return
			}
			pending = nil
			w.WriteHeader(204)
		case "/config":
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	b := newTestAPI(t, func(o *app.Options) { o.EventWorkerURL = server.URL; o.WorkerSecret = strings.Repeat("x", 40) })
	defer b.close()
	tenant, origin, publicID, _ := deletionFixture(t, b)
	base := "/tenants/" + tenant
	dst := b.call("POST", base+"/sites", map[string]string{"name": "Destino", "timezone": "UTC"}, 201)["id"].(string)
	other := b.call("POST", base+"/cameras", map[string]any{"name": "Outra câmera", "siteId": dst, "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://fixture/other"}}}, 201)["id"].(string)
	db := cameraDeletionDB(t, b)
	ctx := context.Background()
	key := cameraTestKey(t, db, tenant, publicID)
	_, err := db.Exec(ctx, `INSERT INTO event_collection_settings(tenant_id,camera_id,revision,source_id,enabled,rules)
 SELECT tenant_id,id,1,source_version,true,'[]' FROM cameras WHERE tenant_id=$1`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Origem", "grants": []app.Grant{{Action: "events.view", Scope: "site", ResourceID: origin}}}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]string{"username": "origin-pilot", "password": "test-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	viewer := independentBrowser(t, b, "origin-pilot", "test-password", tenant)
	path := base + "/analytic-events"
	wait := func(f func() bool) {
		t.Helper()
		deadline := time.Now().Add(18 * time.Second)
		for !f() {
			if time.Now().After(deadline) {
				t.Fatal("collector condition timed out")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	offline.Store(true)
	if _, err = db.Exec(ctx, `UPDATE event_collector_health SET last_contact=now()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return len(b.call("GET", path+"/failures", nil, 200)["items"].([]any)) == 1 })
	readAffected := func(client *browserClient) []any {
		return client.call("GET", path+"/failures", nil, 200)["items"].([]any)[0].(map[string]any)["affected"].([]any)
	}
	if got := readAffected(b); len(got) != 2 {
		t.Fatalf("expected one collector outage for %s and %s: %v", publicID, other, got)
	}
	if got := readAffected(viewer); len(got) != 1 || got[0] != publicID {
		t.Fatalf("collector scope: %v", got)
	}
	receipt := func(index int) eventcollector.Record {
		var at time.Time
		if err := db.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return eventcollector.Record{ID: fmt.Sprintf("%048x", index), Tenant: tenant, Camera: key, Revision: 1, At: at, Alert: &camera.CameraAlert{Kind: "intrusion", State: "active"}}
	}
	old := receipt(1)
	cam := base + "/cameras/" + publicID
	b.call("POST", cam+"/site-changes", map[string]any{"siteId": dst, "revision": 1}, 200)
	b.call("PATCH", cam, map[string]any{"name": "Novo nome", "revision": 2}, 200)
	moved := receipt(2)
	b.call("DELETE", cam, map[string]any{"revision": 3}, 202)
	removed := receipt(3)
	mu.Lock()
	pending = []eventcollector.Record{old, moved, removed}
	mu.Unlock()
	offline.Store(false)
	wait(func() bool { return acks.Load() >= 2 })
	rows := b.call("GET", path, nil, 200)["items"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["siteId"] != dst || rows[0].(map[string]any)["cameraName"] != "Novo nome" {
		t.Fatalf("unexpected destination or duplicate: %v", rows)
	}
	rows = viewer.call("GET", path, nil, 200)["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["siteId"] != origin || rows[0].(map[string]any)["cameraName"] != "Delete me" {
		t.Fatalf("incorrect historical scope: %v", rows)
	}
	rows = b.call("GET", path+"/failures?closed=1", nil, 200)["items"].([]any)
	codes := map[string]int{}
	for _, row := range rows {
		codes[row.(map[string]any)["code"].(string)]++
	}
	if codes["collection_after_deletion"] != 1 || codes["collector_unreachable"] != 1 {
		t.Fatalf("missing historical diagnostics: %v", rows)
	}
}
