package app_test

import (
	"testing"

	"vms.local/backend/internal/app"
)

func TestBulkCameraConfigurationPreservesUnselectedFields(t *testing.T) {
	b := newTestAPI(t)
	tenant, origin, first, _ := deletionFixture(t, b)
	base := "/tenants/" + tenant
	destination := b.call("POST", base+"/sites", map[string]string{"name": "Destino", "timezone": "America/Sao_Paulo"}, 201)["id"].(string)
	other := b.call("POST", base+"/cameras", map[string]any{
		"siteId": origin, "name": "Another camera", "profiles": []map[string]string{
			{"id": "main", "name": "Main", "url": "rtsp://camera/main"},
			{"id": "sub1", "name": "Sub", "url": "rtsp://camera/sub"},
		},
		"recording": map[string]any{"profile": "sub1", "enabled": true, "always": true, "retentionDays": 14},
	}, 201)
	second := other["id"].(string)
	path := base + "/cameras/bulk"
	ids := []string{first, second}
	request := map[string]any{
		"cameraIds": ids, "action": "configure", "siteId": destination, "displayMode": "4:3", "recordingEnabled": true,
		"agenda": map[string]any{"always": false, "windows": []any{}},
	}
	assertBulkCounts(t, b.call("POST", path, request, 200), 2, 0)
	assertBulkCounts(t, b.call("POST", path, request, 200), 0, 2)
	for _, id := range ids {
		current := b.call("GET", base+"/cameras/"+id, nil, 200)
		if current["siteId"] != destination || current["displayMode"] != "4:3" || current["revision"] != float64(2) {
			t.Fatalf("camera %s has incorrect site, mode or revision: %v", id, current)
		}
		config := b.call("GET", base+"/cameras/"+id+"/recording", nil, 200)
		if config["enabled"] != true || config["always"] != false || len(config["schedule"].([]any)) != 0 {
			t.Fatalf("camera %s did not save an enabled recording with an empty agenda: %v", id, config)
		}
		if id == second && (config["profile"] != "sub1" || config["retentionDays"] != float64(14) || current["sourceVersion"] != other["sourceVersion"] || current["name"] != other["name"]) {
			t.Fatalf("bulk editing changed a value outside the patch: %v %v", current, config)
		}
	}
	assertBulkCounts(t, b.call("POST", path, map[string]any{"cameraIds": []string{first}, "action": "deactivate"}, 200), 1, 0)
	if state := b.call("GET", base+"/cameras/"+first+"/recording", nil, 200); state["enabled"] != true {
		t.Fatal("deactivation changed the recording configuration")
	}
	assertBulkCounts(t, b.call("POST", path, map[string]any{"cameraIds": ids, "action": "activate"}, 200), 1, 1)
}

func TestBulkCameraPermissionsAndDeletionAreAtomic(t *testing.T) {
	b := newTestAPI(t)
	tenant, origin, first, _ := deletionFixture(t, b)
	base := "/tenants/" + tenant
	destination := b.call("POST", base+"/sites", map[string]string{"name": "Destino", "timezone": "UTC"}, 201)["id"].(string)
	second := b.call("POST", base+"/cameras", map[string]any{"siteId": destination, "name": "Another camera", "profiles": []map[string]string{
		{"id": "main", "name": "Main", "url": "rtsp://camera/main"},
	}}, 201)["id"].(string)
	operator := b.call("POST", "/admin/operators", map[string]any{"username": "manager", "password": "fixture-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Manage cameras", "grants": []app.Grant{
		{Action: "inventory.manage", Scope: "tenant", ResourceID: tenant},
		{Action: "recording.manage", Scope: "site", ResourceID: origin},
		{Action: "camera.delete", Scope: "site", ResourceID: origin},
	}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.login("manager", "fixture-password")
	b.selectTenant(tenant)
	path := base + "/cameras/bulk"
	ids := []string{first, second}
	moveAndRecord := map[string]any{"cameraIds": []string{first}, "action": "configure", "siteId": destination, "recordingEnabled": true}
	assertBulkIssue(t, b.call("POST", path, moveAndRecord, 422), first, "camera_bulk_recording_forbidden")
	if camera := b.call("GET", base+"/cameras/"+first, nil, 200); camera["siteId"] != origin {
		t.Fatal("move committed despite lacking recording permission on the destination")
	}
	deleteBoth := map[string]any{"cameraIds": ids, "action": "delete"}
	assertBulkIssue(t, b.call("POST", path, deleteBoth, 422), second, "forbidden")
	b.call("GET", base+"/cameras/"+first, nil, 200)
	b.login("admin", "admin")
	b.selectTenant(tenant)
	assertBulkIssue(t, b.call("POST", path, map[string]any{"cameraIds": []string{first, "99999"}, "action": "delete"}, 422), "99999", "camera_not_found")
	b.call("GET", base+"/cameras/"+first, nil, 200)
	assertBulkCounts(t, b.call("POST", path, deleteBoth, 202), 2, 0)
	for _, id := range ids {
		b.call("GET", base+"/cameras/"+id, nil, 404)
	}
	assertBulkCounts(t, b.call("POST", path, deleteBoth, 202), 0, 2)
}

func assertBulkCounts(t *testing.T, response map[string]any, changed, unchanged int) {
	t.Helper()
	if response["changed"] != float64(changed) || response["unchanged"] != float64(unchanged) {
		t.Fatalf("expected counts %d/%d: %v", changed, unchanged, response)
	}
}

func assertBulkIssue(t *testing.T, response map[string]any, id, code string) {
	t.Helper()
	if issues, ok := response["issues"].([]any); ok && len(issues) == 1 {
		if issue, ok := issues[0].(map[string]any); ok && issue["cameraId"] == id && issue["code"] == code {
			return
		}
	}
	t.Fatalf("expected camera %s and issue %s: %v", id, code, response)
}
