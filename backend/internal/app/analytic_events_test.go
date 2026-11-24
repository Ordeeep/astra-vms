package app_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"vms.local/backend/internal/app"
)

func TestAnalyticEventsHistoryPermissionsAndRetention(t *testing.T) {
	b := newTestAPI(t)
	tenant, origin, camera, _ := deletionFixture(t, b)
	base := "/tenants/" + tenant
	path := base + "/analytic-events"
	destination := b.call("POST", base+"/sites", map[string]string{"name": "Galpão", "timezone": "UTC"}, 201)["id"].(string)
	db := cameraDeletionDB(t, b)
	key := cameraTestKey(t, db, tenant, camera)
	now := time.Now().UTC().Truncate(time.Microsecond)
	received := now.Add(-5 * time.Minute)
	cameraAt := now.Add(-48 * time.Hour)
	seed := func(index int, kind, site string, at time.Time) {
		t.Helper()
		_, err := db.Exec(context.Background(), `INSERT INTO analytic_events
 (id,tenant_id,camera_id,site_id,kind,received_at,camera_at,camera_name,site_name,rule_name,simulated)
 SELECT $1,$2,$3,s.id,$5,$6,$7,'Câmera na data do alerta',s.name,'Entrada',true
 FROM sites s WHERE s.id=$4`, fmt.Sprintf("%048x", index), tenant, key, site, kind, at, cameraAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(1, "intrusion", origin, received)
	seed(2, "intrusion", origin, received) // Both receipts survive and paginate at the same instant.
	seed(3, "line_crossing", origin, received.Add(-time.Minute))
	seed(4, "intrusion", origin, now.Add(-91*24*time.Hour))
	b.call("POST", base+"/cameras/"+camera+"/site-changes", map[string]any{"siteId": destination, "revision": 1}, 200)
	seed(5, "line_crossing", destination, received.Add(time.Minute))

	register := func(username string, grant app.Grant) *browserClient {
		t.Helper()
		group := b.call("POST", base+"/groups", map[string]any{"name": username, "grants": []app.Grant{grant}}, 201)["id"].(string)
		user := b.call("POST", "/admin/operators", map[string]any{"username": username, "password": "test-password"}, 201)["id"].(string)
		b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
		return independentBrowser(t, b, username, "test-password", tenant)
	}
	siteViewer := register("origin-events", app.Grant{Action: "events.view", Scope: "site", ResourceID: origin})
	cameraViewer := register("camera-events", app.Grant{Action: "events.view", Scope: "camera", ResourceID: camera})
	liveViewer := register("live-only", app.Grant{Action: "live", Scope: "tenant", ResourceID: tenant})
	liveViewer.call("GET", path, nil, 403)
	liveViewer.call("GET", path+"/filters", nil, 403)
	if siteViewer.call("GET", base+"/capabilities", nil, 200)["events.view"] != true {
		t.Fatal("site grant did not enable Events navigation")
	}
	params := url.Values{"type": {"intrusion"}, "site": {origin}, "camera": {camera}, "limit": {"1"}}
	first := siteViewer.call("GET", path+"?"+params.Encode(), nil, 200)
	rows := first["items"].([]any)
	if len(rows) != 1 || first["nextCursor"] == "" {
		t.Fatalf("expected first of two matching receipts: %v", first)
	}
	firstEvent := rows[0].(map[string]any)
	if firstEvent["id"] != fmt.Sprintf("%048x", 2) || firstEvent["cameraId"] != camera || firstEvent["simulated"] != true {
		t.Fatalf("unexpected event: %v", firstEvent)
	}
	parsedCameraAt, err := time.Parse(time.RFC3339Nano, firstEvent["cameraAt"].(string))
	if err != nil || !parsedCameraAt.Equal(cameraAt) {
		t.Fatalf("camera time was lost: %v", firstEvent)
	}
	for _, field := range []string{"from", "until", "cursor"} {
		value := first[field]
		if field == "cursor" {
			value = first["nextCursor"]
		}
		params.Set(field, value.(string))
	}
	second := siteViewer.call("GET", path+"?"+params.Encode(), nil, 200)
	rows = second["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != fmt.Sprintf("%048x", 1) || second["nextCursor"] != "" {
		t.Fatalf("same-time receipt was not returned on the next page: %v", second)
	}
	if rows := siteViewer.call("GET", path+"?site="+destination, nil, 200)["items"].([]any); len(rows) != 0 {
		t.Fatalf("events from another site were returned: %v", rows)
	}
	before := cameraViewer.call("GET", path, nil, 200)["items"].([]any)
	if len(before) != 4 || before[0].(map[string]any)["siteId"] != destination {
		t.Fatalf("camera grant did not follow the camera across sites: %v", before)
	}

	b.call("PATCH", base+"/cameras/"+camera, map[string]any{"revision": 2, "name": "Novo nome no Galpão"}, 200)
	options := siteViewer.call("GET", path+"/filters", nil, 200)
	if cameras := options["cameras"].([]any); len(cameras) != 1 || cameras[0].(map[string]any)["name"] != "Câmera na data do alerta" {
		t.Fatalf("a site grant revealed the current camera label from another site: %v", options)
	}
	b.call("DELETE", base+"/cameras/"+camera, map[string]int{"revision": 3}, 202)
	options = siteViewer.call("GET", path+"/filters", nil, 200)
	if len(options["cameras"].([]any)) != 1 || len(options["sites"].([]any)) != 1 {
		t.Fatalf("historical filter options escaped their scope or omitted a tombstone: %v", options)
	}
	period := url.Values{"from": {now.Add(-92 * 24 * time.Hour).Format(time.RFC3339Nano)}, "until": {now.Format(time.RFC3339Nano)}}
	after := siteViewer.call("GET", path+"?"+period.Encode(), nil, 200)["items"].([]any)
	if len(after) != 3 || after[0].(map[string]any)["cameraName"] != "Câmera na data do alerta" {
		t.Fatalf("deletion or expiry changed the retained site history: %v", after)
	}
	b.call("GET", path+"?type=unsupported", nil, 422)

	other := b.call("POST", "/admin/tenants", map[string]string{"name": "Outro ambiente"}, 201)["id"].(string)
	b.selectTenant(other)
	b.call("GET", path, nil, 404)
}
