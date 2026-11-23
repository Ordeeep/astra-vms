package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/app"
)

func cameraTestKey(t *testing.T, db *pgx.Conn, tenant, number string) string {
	t.Helper()
	var key string
	if err := db.QueryRow(context.Background(), `SELECT id FROM cameras WHERE tenant_id=$1 AND camera_number::text=$2`, tenant, number).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

func rewindCameraNumbers(t *testing.T, db *pgx.Conn) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `DROP FUNCTION public_camera_grants(text,jsonb);
 DROP TRIGGER assign_camera_number ON cameras;
 DROP FUNCTION assign_camera_number();
 ALTER TABLE cameras DROP COLUMN camera_number;
 ALTER TABLE cameras DROP COLUMN display_mode;
 ALTER TABLE tenants DROP COLUMN last_camera_number;
 DELETE FROM schema_migrations WHERE version>=12;`); err != nil {
		t.Fatal(err)
	}
}

func TestCameraNumbersUpgradeAndTenantIsolation(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	alpha := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	beta := b.call("POST", "/admin/tenants", map[string]string{"name": "Beta"}, 201)["id"].(string)
	createSite := func(tenant string) string {
		b.selectTenant(tenant)
		return b.call("POST", "/tenants/"+tenant+"/sites", map[string]string{"name": "Site", "timezone": "UTC"}, 201)["id"].(string)
	}
	createCamera := func(b *browserClient, tenant, site, name string) string {
		return b.call("POST", "/tenants/"+tenant+"/cameras", map[string]any{
			"name": name, "siteId": site,
			"profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://user:fixture@camera/main"}},
		}, 201)["id"].(string)
	}
	alphaBase, betaBase := "/tenants/"+alpha, "/tenants/"+beta
	alphaSite := createSite(alpha)
	for i := 1; i <= 10; i++ {
		want := strconv.Itoa(i)
		if got := createCamera(b, alpha, alphaSite, "Alpha "+want); got != want {
			t.Fatalf("camera %d received %s", i, got)
		}
	}
	b.call("DELETE", alphaBase+"/cameras/1", map[string]int{"revision": 1}, 202)
	if got := createCamera(b, alpha, alphaSite, "Alpha 11"); got != "11" {
		t.Fatalf("deleted number was reused: %s", got)
	}
	b.call("PATCH", alphaBase+"/cameras/3", map[string]any{"name": "Alpha 3", "active": false, "revision": 1}, 200)
	db := cameraDeletionDB(t, b)
	key := cameraTestKey(t, db, alpha, "2")
	mosaic := b.call("POST", alphaBase+"/mosaics", map[string]any{"name": "Legacy mosaic", "visibility": "personal",
		"positions": []app.Position{{CameraID: key, Profile: "main"}}}, 201)["id"].(string)
	group := b.call("POST", "/admin/installation-groups", map[string]any{"name": "Alpha viewer", "grants": []app.Grant{
		{Action: "playback", Scope: "camera", ResourceID: key},
	}}, 201)["id"].(string)
	viewer := b.call("POST", "/admin/operators", map[string]any{
		"username": "camera-viewer", "password": "fixture-password", "installationGroups": []string{group},
	}, 201)["id"].(string)
	b.call("PUT", alphaBase+"/operators/"+viewer, map[string]any{"groups": []string{}, "revision": 0}, 200)
	betaSite := createSite(beta)
	if got := createCamera(b, beta, betaSite, "Beta 1"); got != "1" {
		t.Fatalf("second tenant received %s", got)
	}
	b.call("PUT", betaBase+"/operators/"+viewer, map[string]any{"groups": []string{}, "revision": 0}, 200)
	b.close()
	rewindCameraNumbers(t, db)
	if _, err := db.Exec(context.Background(), `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata)
 SELECT 'legacy-segment',tenant_id,id,site_id,source_version,'main','2026-11-19T12:00:00Z','2026-11-19T12:00:01Z',10,4096,jsonb_build_object('camera',id)
 FROM cameras WHERE tenant_id=$1 AND id=$2`, alpha, key); err != nil {
		t.Fatal(err)
	}

	restarted := newTestAPI(t, func(o *app.Options) { *o = b.options })
	restarted.login("admin", "admin")
	restarted.selectTenant(alpha)
	for _, ref := range []string{"1", key} {
		if got := restarted.call("GET", alphaBase+"/cameras/"+ref, nil, 200); got["id"] != "1" || got["name"] != "Alpha 2" || got["displayMode"] != "contain" {
			t.Fatalf("migrated camera mismatch: %v", got)
		}
	}
	if got := restarted.call("GET", alphaBase+"/cameras/2", nil, 200); got["name"] != "Alpha 3" || got["active"] != false {
		t.Fatalf("inactive camera was not numbered in order: %v", got)
	}
	positions := restarted.call("GET", alphaBase+"/mosaics/"+mosaic, nil, 200)["positions"].([]any)
	if positions[0].(map[string]any)["cameraId"] != "1" {
		t.Fatalf("mosaic did not keep its camera: %v", positions)
	}
	var migratedGrant map[string]any
	for _, item := range restarted.call("GET", "/admin/installation-groups", nil, 200)["items"].([]any) {
		if row := item.(map[string]any); row["id"] == group {
			migratedGrant = row["grants"].([]any)[0].(map[string]any)
			break
		}
	}
	if migratedGrant["resourceId"] != "1" || migratedGrant["tenantId"] != alpha {
		t.Fatalf("migrated grant lost its tenant or camera: %v", migratedGrant)
	}
	restarted.call("POST", "/admin/installation-groups", map[string]any{"name": "Ambiguous camera", "grants": []app.Grant{
		{Action: "playback", Scope: "camera", ResourceID: "1"},
	}}, 422)
	viewerClient := independentBrowser(t, restarted, "camera-viewer", "fixture-password", alpha)
	viewerClient.call("GET", alphaBase+"/cameras/1", nil, 200)
	restarted.call("PUT", "/admin/installation-groups/"+group, map[string]any{"name": "Alpha viewer", "revision": 1, "grants": []app.Grant{
		{Action: "playback", Scope: "camera", TenantID: alpha, ResourceID: "1"},
	}}, 200)
	timeline := viewerClient.call("GET", alphaBase+"/cameras/1/timeline?start=2026-11-19T12:00:00Z&end=2026-11-19T12:00:01Z", nil, 200)["items"].([]any)
	if len(timeline) != 1 || timeline[0].(map[string]any)["cameraId"] != "1" {
		t.Fatalf("legacy recording is unavailable under its new ID: %v", timeline)
	}
	viewerClient.selectTenant(beta)
	viewerClient.call("GET", betaBase+"/cameras/1", nil, 404)
	restarted.selectTenant(beta)
	restarted.call("GET", betaBase+"/cameras/"+key, nil, 404)
	if got := restarted.call("GET", betaBase+"/cameras/1", nil, 200); got["name"] != "Beta 1" {
		t.Fatalf("second tenant's camera changed: %v", got)
	}
	restarted.selectTenant(alpha)
	if got := createCamera(restarted, alpha, alphaSite, "After upgrade"); got != "11" {
		t.Fatalf("numbering did not continue after the backfill: %s", got)
	}
	rows := restarted.call("GET", alphaBase+"/cameras?cursor=9", nil, 200)["items"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] != "10" || rows[1].(map[string]any)["id"] != "11" {
		t.Fatalf("cursor did not follow numeric order: %v", rows)
	}
}
