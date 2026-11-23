package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

func TestExportTasksKeepPersonalHistoryAndStopOnlyOwnActiveJobs(t *testing.T) {
	stops := make(chan string, 10)
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/archive/usage":
			w.Write([]byte(`{"export":0}`))
		case r.Method == "DELETE" && r.URL.Path == "/archive/exports/running-old":
			stops <- r.URL.Query().Get("tenant")
			w.WriteHeader(204)
		default:
			w.WriteHeader(503)
		}
	}))
	t.Cleanup(worker.Close)
	admin := newTestAPI(t, func(o *app.Options) { o.WorkerURL, o.WorkerSecret = worker.URL, "fixture-export-tasks" })
	tenant, site, camera, actor := deletionFixture(t, admin)
	base := "/tenants/" + tenant
	otherTenant := admin.call("POST", "/admin/tenants", map[string]string{"name": "Other tenant"}, 201)["id"].(string)
	otherActor := admin.call("POST", "/admin/operators", map[string]string{"username": "other-exporter", "password": "fixture-password"}, 201)["id"].(string)
	group := admin.call("POST", base+"/groups", map[string]any{"name": "Exporters", "grants": []app.Grant{
		{Action: "playback", Scope: "tenant", ResourceID: tenant}, {Action: "export", Scope: "tenant", ResourceID: tenant},
	}}, 201)["id"].(string)
	admin.call("PUT", base+"/operators/"+otherActor, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	other := independentBrowser(t, admin, "other-exporter", "fixture-password", tenant)

	ctx := context.Background()
	db := cameraDeletionDB(t, admin)
	key := cameraTestKey(t, db, tenant, camera)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	end := start.Add(time.Second)
	index := media.SegmentIndex{Tenant: tenant, Site: site, Camera: key, Source: "source", Profile: "main", Start: start, End: end,
		Parts: []media.PartIndex{{ArchivePart: media.ArchivePart{Start: start, End: end, Width: 320, Height: 240, Timescale: 90000, Frames: 1, Durations: []uint32{90000}}}}}
	metadata, _ := json.Marshal(index)
	exec(`INSERT INTO recording_policies(tenant_id,quota_bytes,export_bytes,retention_days,export_hours) VALUES($1,0,8388608,10,24)`, tenant)
	exec(`INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata)
 VALUES('export-source',$1,$2,$3,'source','main',$4,$5,10,4096,$6)`, tenant, key, site, start, end, metadata)
	exec(`UPDATE operators SET name='Solicitante original' WHERE id=$1`, actor)

	seed := func(id, environment, owner, status, code, result string, created, expires time.Time) {
		t.Helper()
		origin := index
		origin.Tenant = environment
		origin.Parts = nil
		plan, _ := json.Marshal(media.ExportPlan{Tenant: environment, Camera: key, Start: start, End: end,
			Clips: []media.ExportClip{{ID: "export-source", Index: origin}}})
		exec(`INSERT INTO export_jobs(id,tenant_id,actor_id,preview_id,idempotency_key,plan,status,code,result,created_at,expires)
 VALUES($1,$2,$3,'preview',$1,$4,$5,$6,$7::jsonb,$8,$9)`, id, environment, owner, plan, status, code, result, created, expires)
	}
	seed("running-old", tenant, actor, "running", "", `{"frames":42,"total":100}`, start.Add(-time.Hour), end.Add(time.Hour))
	seed("queued-old", tenant, actor, "queued", "", `{}`, start.Add(-time.Minute), end.Add(time.Hour))
	seed("other-actor", tenant, otherActor, "running", "", `{}`, start, end.Add(time.Hour))
	seed("other-tenant", otherTenant, actor, "running", "", `{}`, start, end.Add(time.Hour))
	for n := range 30 {
		seed(fmt.Sprintf("failed-%02d", n), tenant, actor, "failed", "export_failed", `{}`, start, end.Add(time.Hour))
	}
	seed("expired-video", tenant, actor, "expired", "", `{"status":"completed"}`, start.Add(time.Second), start)
	seed("expired-cancelled", tenant, actor, "expired", "camera_deleted", `{}`, start.Add(time.Second), start)
	seed("ready", tenant, actor, "completed", "", `{"status":"completed","content":"fixture-object"}`, start.Add(time.Second), end.Add(time.Hour))

	preview := admin.call("POST", base+"/export-previews", map[string]any{"cameraId": camera, "start": start, "end": end, "timezone": "UTC"}, 201)
	job := admin.call("POST", base+"/exports", map[string]any{"previewId": preview["id"], "revision": 1, "idempotencyKey": "export-task-history-fixture"}, 202)["id"].(string)
	exec(`UPDATE cameras SET name='Nome atual' WHERE id=$1`, key)
	exec(`UPDATE operators SET name='Nome atualizado' WHERE id=$1`, actor)

	items := func(page map[string]any) map[string]map[string]any {
		byID := map[string]map[string]any{}
		for _, raw := range page["items"].([]any) {
			row := raw.(map[string]any)
			byID[row["id"].(string)] = row
		}
		return byID
	}
	first := admin.call("GET", base+"/export-tasks", nil, 200)
	front := items(first)
	if len(front) != 30 || first["hasActive"] != true || front["running-old"] != nil || front["other-actor"] != nil || front["other-tenant"] != nil {
		t.Fatal("history pagination or personal scope did not match")
	}
	if row := front[job]; row["cameraName"] != "Delete me" || row["author"] != "Solicitante original" || row["start"] != start.Format(time.RFC3339) || row["end"] != end.Format(time.RFC3339) {
		t.Fatalf("request history lost its original metadata: %+v", row)
	}
	if front["expired-video"]["status"] != "completed" || front["expired-video"]["downloadExpired"] != true ||
		front["expired-cancelled"]["status"] != "cancelled" || front["ready"]["canDownload"] != true {
		t.Fatal("download expiry changed a historical outcome or a valid download")
	}
	cursor := first["nextCursor"].(string)
	if row := items(admin.call("GET", base+"/export-tasks?cursor="+cursor, nil, 200))["running-old"]; row["frames"] != float64(42) || row["total"] != float64(100) {
		t.Fatalf("running task or progress lost across pages: %+v", row)
	}
	if own := items(other.call("GET", base+"/export-tasks", nil, 200)); len(own) != 1 || own["other-actor"] == nil {
		t.Fatal("another operator received someone else's history")
	}
	admin.call("GET", base+"/exports/other-actor", nil, 404)
	other.call("GET", base+"/exports/ready/content", nil, 404)
	admin.call("POST", base+"/exports/other-actor/cancel", nil, 404)
	admin.call("POST", base+"/exports/ready/cancel", nil, 409)
	admin.call("POST", base+"/exports/"+job+"/cancel", nil, 204)
	if got := admin.call("POST", base+"/exports/cancel", nil, 200)["cancelled"]; got != float64(2) {
		t.Fatalf("bulk did not cancel both active tasks outside the first page: %v", got)
	}
	select {
	case stoppedTenant := <-stops:
		if stoppedTenant != tenant {
			t.Fatal("worker cancellation used the wrong tenant")
		}
	default:
		t.Fatal("running export did not receive its worker stop")
	}
	if got := admin.call("POST", base+"/exports/cancel", nil, 200)["cancelled"]; got != float64(0) {
		t.Fatal("retry cancelled additional jobs")
	}
	if got := items(other.call("GET", base+"/export-tasks", nil, 200))["other-actor"]["status"]; got != "running" {
		t.Fatal("bulk cancellation changed another operator's task")
	}
	var separateTenantStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM export_jobs WHERE id='other-tenant'`).Scan(&separateTenantStatus); err != nil || separateTenantStatus != "running" {
		t.Fatal("bulk cancellation changed another tenant", err)
	}
	var audits int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='export.cancelled' AND actor_id=$1 AND tenant_id=$2`, actor, tenant).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("expected one audit per cancellation, got %d (%v)", audits, err)
	}
}
