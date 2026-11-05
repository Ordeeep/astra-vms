package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

func cameraDeletionDB(t *testing.T, b *browserClient) *pgx.Conn {
	t.Helper()
	cfg, err := pgx.ParseConfig(b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = b.options.Schema
	db, err := pgx.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}
func deletionFixture(t *testing.T, b *browserClient) (string, string, string, string) {
	t.Helper()
	actor := b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Original", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Delete me", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Main", "url": "rtsp://fixture:private@camera/main"}}}, 201)["id"].(string)
	return tenant, site, camera, actor
}
func TestCameraDeletionPermissionTenantRevisionAndRevocation(t *testing.T) {
	b := newTestAPI(t)
	tenant, site, camera, actor := deletionFixture(t, b)
	base := "/tenants/" + tenant
	path := base + "/cameras/" + camera
	db := cameraDeletionDB(t, b)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	// The current-site deletion grant covers the whole camera history in this tenant.
	currentSite := b.call("POST", base+"/sites", map[string]string{"name": "Current", "timezone": "UTC"}, 201)["id"].(string)
	b.call("POST", path+"/site-changes", map[string]any{"siteId": currentSite, "revision": 1}, 200)
	exec(`INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata) SELECT 'old-segment',tenant_id,id,$3,source_version,'main',now()-interval '1 hour',now(),10,4096,'{}' FROM cameras WHERE tenant_id=$1 AND id=$2`, tenant, camera, site)
	exec(`INSERT INTO playback_sessions(id,tenant_id,camera_id,context_id,session_hash,start_at,end_at,expires) VALUES('playing',$1,$2,'context','session',now()-interval '1 hour',now(),now()+interval '1 hour')`, tenant, camera)
	exec(`INSERT INTO live_sessions(id,tenant_id,camera_id,context_id,session_hash,source_version,profile_id,worker_token,media_path,expires) SELECT 'watching',tenant_id,id,'context','session',source_version,'main','lease','path',now()+interval '1 hour' FROM cameras WHERE tenant_id=$1 AND id=$2`, tenant, camera)
	plan, _ := json.Marshal(media.ExportPlan{Tenant: tenant, Camera: camera, Clips: []media.ExportClip{{ID: "old-segment", Index: media.SegmentIndex{Tenant: tenant, Camera: camera, Site: site}}}})
	for _, status := range []string{"queued", "running", "completed"} {
		exec(`INSERT INTO export_jobs(id,tenant_id,actor_id,preview_id,idempotency_key,plan,status,expires) VALUES($1,$2,$3,'preview',$1,$4,$5,now()+interval '1 hour')`, status, tenant, actor, plan, status)
	}
	b.call("POST", base+"/mosaics", map[string]any{"name": "Keep layout", "visibility": "personal", "positions": []map[string]string{{"cameraId": camera, "profile": "main"}, {"cameraId": "", "profile": ""}}}, 201)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "editor", "password": "editor-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Editor only", "grants": []app.Grant{{Action: "inventory.manage", Scope: "site", ResourceID: currentSite}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	editor := independentBrowser(t, b, "editor", "editor-password", tenant)
	if editor.call("GET", path, nil, 200)["capabilities"].(map[string]any)["camera.delete"] != false {
		t.Fatal("editing granted deletion")
	}
	editor.call("DELETE", path, map[string]int{"revision": 2}, 403)
	b.call("DELETE", path, map[string]int{"revision": 1}, 412)
	if b.call("GET", path, nil, 200)["active"] != true {
		t.Fatal("failed deletion changed camera")
	}
	otherTenant := b.call("POST", "/admin/tenants", map[string]string{"name": "Other tenant"}, 201)["id"].(string)
	b.selectTenant(otherTenant)
	b.call("DELETE", "/tenants/"+otherTenant+"/cameras/"+camera, map[string]int{"revision": 2}, 404)
	b.selectTenant(tenant)
	erase := b.call("POST", base+"/groups", map[string]any{"name": "Delete current site", "grants": []app.Grant{{Action: "camera.delete", Scope: "site", ResourceID: currentSite}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{erase}, "revision": 1}, 200)
	editor.call("DELETE", path, map[string]int{"revision": 2}, 202)
	editor.call("DELETE", path, map[string]int{"revision": 2}, 202) // lost response/retry is idempotent
	b.call("GET", path, nil, 404)
	if len(b.call("GET", base+"/cameras", nil, 200)["items"].([]any)) != 0 {
		t.Fatal("deleted camera visible")
	}
	b.call("PATCH", path, map[string]any{"name": "Restore", "active": true, "revision": 3}, 404)
	b.call("PATCH", path+"/configuration", map[string]any{"name": "Restore", "siteId": currentSite, "active": true, "revision": 3}, 404)
	b.call("GET", path+"/source-versions", nil, 404)
	var connections, playing, watching, cancelled, ready, requests, audits int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM camera_sources WHERE tenant_id=$1 AND camera_id=$2 AND (octet_length(secrets)>0 OR connection<>'null' OR profiles<>'[]')),
 (SELECT count(*) FROM playback_sessions WHERE tenant_id=$1 AND camera_id=$2),
 (SELECT count(*) FROM live_sessions WHERE tenant_id=$1 AND camera_id=$2 AND expires>now()),
 (SELECT count(*) FROM export_jobs WHERE tenant_id=$1 AND status='cancelled'),
 (SELECT count(*) FROM export_jobs WHERE tenant_id=$1 AND status='completed'),
 (SELECT count(*) FROM camera_deletions WHERE tenant_id=$1 AND camera_id=$2),
 (SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND action='camera.deleted' AND resource_id=$2)`, tenant, camera).Scan(&connections, &playing, &watching, &cancelled, &ready, &requests, &audits); err != nil {
		t.Fatal(err)
	}
	if connections != 0 || playing != 0 || watching != 0 || cancelled != 2 || ready != 1 || requests != 1 || audits != 1 {
		t.Fatalf("revocation mismatch: %d %d %d %d %d %d %d", connections, playing, watching, cancelled, ready, requests, audits)
	}
	var positions []byte
	if err := db.QueryRow(ctx, "SELECT positions FROM mosaics WHERE tenant_id=$1", tenant).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	var slots []app.Position
	json.Unmarshal(positions, &slots)
	if len(slots) != 2 || slots[0].CameraID != "" {
		t.Fatal("saved layout kept deleted camera")
	}
	// Requests that began before deletion cannot create another consumer afterwards.
	if _, err := db.Exec(ctx, `INSERT INTO playback_sessions(id,tenant_id,camera_id,context_id,session_hash,start_at,end_at,expires) VALUES('late',$1,$2,'context','session',now(),now()+interval '1 hour',now()+interval '1 hour')`, tenant, camera); err == nil {
		t.Fatal("tombstone accepted a late playback session")
	}
}

func TestCameraDeletionCleanupResumesAfterWorkerFailureAndCoreRestart(t *testing.T) {
	var available atomic.Bool
	var called atomic.Int64
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/camera-cleanup" {
			called.Add(1)
			if !available.Load() {
				http.Error(w, "offline", 503)
				return
			}
			json.NewEncoder(w).Encode(media.CameraCleanupPage{Done: true})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		w.WriteHeader(204)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "fixture-worker" })
	tenant, _, camera, _ := deletionFixture(t, b)
	db := cameraDeletionDB(t, b)
	b.call("DELETE", "/tenants/"+tenant+"/cameras/"+camera, map[string]int{"revision": 1}, 202)
	waitFor := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("cleanup condition did not become true")
	}
	waitFor(func() bool {
		var attempts int
		db.QueryRow(context.Background(), "SELECT attempts FROM camera_deletions WHERE tenant_id=$1 AND camera_id=$2", tenant, camera).Scan(&attempts)
		return attempts > 0 && called.Load() > 0
	})
	var pending int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='camera.cleanup_pending' AND outcome='failed'", tenant, camera).Scan(&pending); err != nil || pending != 1 {
		t.Fatal("failure not audited", err, pending)
	}
	saved := b.options
	b.close()
	available.Store(true)
	restarted := newTestAPI(t, func(o *app.Options) { *o = saved })
	restarted.login("admin", "admin")
	restarted.selectTenant(tenant)
	restarted.call("GET", "/tenants/"+tenant+"/cameras/"+camera, nil, 404)
	waitFor(func() bool {
		var status string
		db.QueryRow(context.Background(), "SELECT status FROM camera_deletions WHERE tenant_id=$1 AND camera_id=$2", tenant, camera).Scan(&status)
		return status == "completed"
	})
	var complete int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='camera.cleanup_completed'", tenant, camera).Scan(&complete); err != nil || complete != 1 {
		t.Fatal("completion not audited", err, complete)
	}
}
