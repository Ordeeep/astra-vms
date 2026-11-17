package app_test

import (
	"context"
	"testing"

	"vms.local/backend/internal/app"
)

func recordingDraft(enabled bool, profile string, revision int) map[string]any {
	return map[string]any{"enabled": enabled, "profile": profile, "always": true, "schedule": []any{}, "retentionDays": nil, "revision": revision}
}

func TestCameraRecordingAtomicCreateAndEdit(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Matriz", "timezone": "UTC"}, 201)["id"].(string)
	profiles := []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera/main"}, {"id": "sub1", "name": "Sub", "url": "rtsp://camera/sub"}}
	input := map[string]any{"name": "Atomic", "siteId": site, "profiles": profiles, "recording": recordingDraft(true, "missing", 0)}
	b.call("POST", base+"/cameras", input, 422)
	if len(b.call("GET", base+"/cameras", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("invalid recording left a partially created camera")
	}
	input["recording"] = recordingDraft(true, "sub1", 0)
	created := b.call("POST", base+"/cameras", input, 201)
	path := base + "/cameras/" + created["id"].(string)
	if b.call("GET", path+"/recording", nil, 200)["profile"] != "sub1" {
		t.Fatal("creation did not save recording")
	}
	status := b.call("GET", base+"/recording-status", nil, 200)
	if status["storageConfigured"] != false || status["items"].([]any)[0].(map[string]any)["code"] != "storage_not_configured" {
		t.Fatal("missing storage was not signaled")
	}
	source := map[string]any{"connectionType": "rtsp", "profiles": profiles[:1]}
	edit := map[string]any{"name": "After", "siteId": site, "active": true, "revision": 1, "source": source}
	b.call("PATCH", path+"/configuration", edit, 409)
	current := b.call("GET", path, nil, 200)
	if current["name"] != "Atomic" || current["connectionRevision"] != float64(1) {
		t.Fatal("incompatible source edit partially saved")
	}
	edit["recording"] = recordingDraft(false, "sub1", 0)
	b.call("PATCH", path+"/configuration", edit, 412)
	if b.call("GET", path, nil, 200)["name"] != "Atomic" {
		t.Fatal("stale recording revision partially saved camera")
	}
	edit["recording"] = recordingDraft(false, "sub1", 1)
	current = b.call("PATCH", path+"/configuration", edit, 200)
	if current["name"] != "After" || len(current["profiles"].([]any)) != 1 {
		t.Fatal("joint source and recording change was not applied")
	}
	if b.call("GET", path+"/recording", nil, 200)["enabled"] != false {
		t.Fatal("recording was not disabled explicitly")
	}
	// Legacy source endpoints must obey the same active-profile invariant.
	b.call("PUT", path+"/recording", recordingDraft(true, "main", 2), 200)
	b.call("PATCH", path+"/configuration", map[string]any{"name": "Still recording", "siteId": site, "active": true, "revision": 2, "recording": recordingDraft(true, "main", 2)}, 412)
	if b.call("GET", path, nil, 200)["name"] != "After" {
		t.Fatal("concurrent recording change was overwritten")
	}
}

func TestCameraRecordingIndependentPermissions(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Matriz", "timezone": "UTC"}, 201)["id"].(string)
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Restricted", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera/main"}, {"id": "sub1", "name": "Sub", "url": "rtsp://camera/sub"}}, "recording": recordingDraft(true, "sub1", 0)}, 201)
	cid := created["id"].(string)
	path := base + "/cameras/" + cid
	operator := b.call("POST", "/admin/operators", map[string]string{"username": "recorder", "password": "fixture-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Recording only", "grants": []app.Grant{{Action: "recording.manage", Scope: "camera", ResourceID: cid}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	b.login("recorder", "fixture-password")
	b.selectTenant(tenant)
	input := map[string]any{"recordingOnly": true, "revision": 1, "recording": recordingDraft(false, "sub1", 1)}
	b.call("PATCH", path+"/configuration", input, 200)
	input["name"] = "Not allowed"
	b.call("PATCH", path+"/configuration", input, 422)
	delete(input, "name")
	input["revision"] = 0
	input["recording"] = recordingDraft(true, "sub1", 2)
	b.call("PATCH", path+"/configuration", input, 412)
	if b.call("GET", path, nil, 200)["name"] != "Restricted" {
		t.Fatal("recording-only operator edited inventory")
	}
	b.login("admin", "admin")
	b.selectTenant(tenant)
	inventory := b.call("POST", base+"/groups", map[string]any{"name": "Inventory only", "grants": []app.Grant{{Action: "inventory.manage", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{inventory}, "revision": 1}, 200)
	b.call("PUT", path+"/recording", recordingDraft(true, "sub1", 2), 200)
	b.login("recorder", "fixture-password")
	b.selectTenant(tenant)
	edit := map[string]any{"name": "Not applied", "siteId": site, "active": true, "revision": 1, "source": map[string]any{"connectionType": "rtsp", "profiles": []map[string]string{{"id": "main", "name": "Principal"}}}}
	b.call("PATCH", path+"/configuration", edit, 409)
	if b.call("GET", path, nil, 200)["name"] != "Restricted" {
		t.Fatal("inventory-only edit invalidated active recording")
	}
	edit["recording"] = recordingDraft(false, "sub1", 3)
	b.call("PATCH", path+"/configuration", edit, 403)
	delete(edit, "source")
	delete(edit, "recording")
	edit["name"] = "Metadata allowed"
	b.call("PATCH", path+"/configuration", edit, 200)
}

func TestCameraRecordingPendingProfilesAndRetentionRollback(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Matriz", "timezone": "UTC"}, 201)["id"].(string)
	created := b.call("POST", base+"/cameras", map[string]any{"name": "Offline vendor", "siteId": site, "connectionType": "dahua", "connection": map[string]any{"host": "127.0.0.1", "httpPort": 1, "protocol": "http", "username": "fixture", "password": "fixture-password"}, "recording": recordingDraft(false, "main", 0)}, 201)
	path := base + "/cameras/" + created["id"].(string)
	if len(created["profiles"].([]any)) != 0 {
		t.Fatal("fixture unexpectedly discovered profiles")
	}
	b.call("PATCH", path+"/configuration", map[string]any{"recordingOnly": true, "revision": 1, "recording": recordingDraft(true, "main", 1)}, 422)
	created = b.call("POST", base+"/cameras", map[string]any{"name": "Archive", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://camera/main"}}, "recording": recordingDraft(false, "main", 0)}, 201)
	cid := created["id"].(string)
	path = base + "/cameras/" + cid
	db := cameraDeletionDB(t, b)
	if _, err := db.Exec(context.Background(), `INSERT INTO recording_policies(tenant_id,quota_bytes,export_bytes,retention_days,export_hours) VALUES($1,0,0,10,24)`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata) SELECT 'segment',tenant_id,id,site_id,source_version,'main',now()-interval '5 days',now()-interval '4 days',10,4096,'{}' FROM cameras WHERE tenant_id=$1 AND id=$2`, tenant, cid); err != nil {
		t.Fatal(err)
	}
	recording := recordingDraft(false, "main", 1)
	recording["retentionDays"] = 1
	edit := map[string]any{"name": "Changed", "siteId": site, "active": true, "revision": 1, "recording": recording}
	impact := b.call("PATCH", path+"/configuration", edit, 409)
	if impact["eligibleBytes"] != float64(4096) {
		t.Fatal("missing retention impact")
	}
	if b.call("GET", path, nil, 200)["name"] != "Archive" {
		t.Fatal("retention confirmation partially saved camera")
	}
	recording["confirmImpact"] = true
	b.call("PATCH", path+"/configuration", edit, 200)
}
