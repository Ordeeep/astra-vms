package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

func TestScheduledCaptureToAuthorizedTemporalPlayback(t *testing.T) {
	source := os.Getenv("VMS_TEST_RTSP")
	if source == "" || os.Getenv("VMS_TEST_DB") == "" {
		t.Skip("PostgreSQL and synthetic RTSP required")
	}
	archive, err := media.NewArchiveService(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	guard := media.NewGuard(media.GuardOptions{ServiceSecret: "recording-integration-private-secret", Archive: archive})
	var dropQuotaResponse, quotaResponseLost, quotaReconciled atomic.Bool
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/limits" && r.Method == "PUT" {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var input struct {
				Kind  string
				Bytes int64
			}
			json.Unmarshal(raw, &input)
			if input.Kind == "recording" && input.Bytes == 32<<20 && dropQuotaResponse.CompareAndSwap(true, false) {
				record := httptest.NewRecorder()
				guard.ServeHTTP(record, r)
				if record.Code != 204 {
					t.Errorf("fault setup quota apply: %d", record.Code)
				}
				quotaResponseLost.Store(true)
				connection, _, _ := w.(http.Hijacker).Hijack()
				connection.Close()
				return
			}
			if input.Kind == "recording" && input.Bytes == 16<<20 && quotaResponseLost.Load() {
				quotaReconciled.Store(true)
			}
		}
		guard.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { worker.Close(); archive.Close() })
	b := newTestAPI(t, func(o *app.Options) {
		o.WorkerURL = worker.URL
		o.WorkerSecret = "recording-integration-private-secret"
	})
	user := b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	b.call("POST", "/admin/storage", map[string]any{"path": t.TempDir(), "volumeLimit": 32 << 20, "margin": 1 << 20}, 200)
	b.call("PUT", base+"/recording-policy", map[string]any{"quotaBytes": 16 << 20, "exportBytes": 8 << 20, "retentionDays": 10, "exportHours": 24, "revision": 0}, 200)
	dropQuotaResponse.Store(true)
	b.call("PUT", base+"/recording-policy", map[string]any{"quotaBytes": 32 << 20, "exportBytes": 8 << 20, "retentionDays": 10, "exportHours": 24, "revision": 1}, 409)
	reconcileDeadline := time.Now().Add(6 * time.Second)
	for !quotaReconciled.Load() && time.Now().Before(reconcileDeadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !quotaReconciled.Load() {
		t.Fatal("lost quota response did not fence and reconcile writer")
	}
	if b.call("GET", base+"/recording-policy", nil, 200)["policy"].(map[string]any)["quotaBytes"] != float64(16<<20) {
		t.Fatal("failed quota mutation changed SQL policy")
	}
	site := b.call("POST", base+"/sites", map[string]any{"name": "Site de origem", "address": "", "timezone": "America/Sao_Paulo"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Captura temporal", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": source}}}, 201)["id"].(string)
	start := time.Now().UTC()
	b.call("PUT", base+"/cameras/"+camera+"/recording", map[string]any{"enabled": true, "profile": "main", "schedule": []any{}, "always": true, "revision": 0}, 200)
	time.Sleep(6 * time.Second)
	b.call("PUT", base+"/cameras/"+camera+"/recording", map[string]any{"enabled": false, "profile": "main", "schedule": []any{}, "always": true, "revision": 1}, 200)
	end := time.Now().UTC()
	query := "?start=" + url.QueryEscape(start.Format(time.RFC3339Nano)) + "&end=" + url.QueryEscape(end.Format(time.RFC3339Nano))
	var timeline map[string]any
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		timeline = b.call("GET", base+"/cameras/"+camera+"/timeline"+query, nil, 200)
		if len(timeline["items"].([]any)) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	items := timeline["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("no catalog after capture: %+v", b.call("GET", base+"/recording-status", nil, 200))
	}
	if items[0].(map[string]any)["siteId"] != site {
		t.Fatal("historical site lost")
	}
	siteB := b.call("POST", base+"/sites", map[string]any{"name": "Destino", "timezone": "UTC"}, 201)["id"].(string)
	b.call("POST", base+"/cameras/"+camera+"/site-changes", map[string]any{"revision": 1, "siteId": siteB}, 200)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Histórico do site anterior", "grants": []app.Grant{{Action: "playback", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 1}, 200)
	found := b.call("GET", base+"/recording-cameras", nil, 200)["items"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["id"] != camera {
		t.Fatal("historical site operator cannot discover moved camera")
	}
	start, _ = time.Parse(time.RFC3339Nano, items[0].(map[string]any)["start"].(string))
	session := b.call("POST", base+"/playback-sessions", map[string]any{"cameraId": camera, "start": start, "end": end}, 201)
	raw := b.mediaGet(session["manifest"].(string), "", 200)
	playlist := string(raw)
	if !strings.Contains(playlist, "#EXT-X-PROGRAM-DATE-TIME:") || !strings.Contains(playlist, "#EXT-X-ENDLIST") {
		t.Fatalf("invalid temporal playlist: %s", playlist)
	}
	var init, part string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "#EXT-X-MAP:") && init == "" {
			init = strings.Split(line, "\"")[1]
		}
		if strings.HasPrefix(line, "/api/") && part == "" {
			part = line
		}
	}
	data := append(b.mediaGet(init, "", 200), b.mediaGet(part, "", 200)...)
	decode := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-f", "null", "-")
	decode.Stdin = bytes.NewReader(data)
	if out, err := decode.CombinedOutput(); err != nil {
		t.Fatalf("public archive bytes not decodable: %v %s", err, out)
	}
	partial := b.mediaGet(part, "bytes=0-15", 206)
	if len(partial) != 16 {
		t.Fatal("range not restricted")
	}
	exportWindow := map[string]any{"cameraId": camera, "start": start.Add(-500 * time.Millisecond), "end": end.Add(500 * time.Millisecond), "timezone": "America/Sao_Paulo"}
	b.call("POST", base+"/export-previews", exportWindow, 403)
	b.call("PUT", base+"/groups/"+group, map[string]any{"name": "Histórico e exportação de origem", "revision": 1, "grants": []app.Grant{{Action: "playback", Scope: "site", ResourceID: site}, {Action: "export", Scope: "site", ResourceID: site}}}, 200)
	preview := b.call("POST", base+"/export-previews", exportWindow, 201)
	if len(preview["gaps"].([]any)) < 1 {
		t.Fatal("export preview omitted missing time")
	}
	request := map[string]any{"previewId": preview["id"], "revision": 1, "idempotencyKey": "integration-export-0001"}
	job := b.call("POST", base+"/exports", request, 202)
	retry := b.call("POST", base+"/exports", request, 202)
	if job["id"] != retry["id"] {
		t.Fatal("idempotent retry created another export")
	}
	// More requests than pool connections must complete without nested-pool
	// acquisition while holding the serialized authorization transaction.
	results := make(chan error, 16)
	for range 16 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			raw, _ := json.Marshal(request)
			req, _ := http.NewRequestWithContext(ctx, "POST", b.server+"/api/v1"+base+"/exports", bytes.NewReader(raw))
			req.Header.Set("X-CSRF-Token", b.csrf)
			req.Header.Set("X-VMS-Context", b.view)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", b.server)
			res, e := b.client.Do(req)
			if e != nil {
				results <- e
				return
			}
			defer res.Body.Close()
			var received map[string]any
			if e = json.NewDecoder(res.Body).Decode(&received); e != nil {
				results <- e
				return
			}
			if res.StatusCode != 202 || received["id"] != job["id"] {
				results <- fmt.Errorf("concurrent retry: %d %+v", res.StatusCode, received)
				return
			}
			results <- nil
		}()
	}
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	jobPath := base + "/exports/" + job["id"].(string)
	deadline = time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		job = b.call("GET", jobPath, nil, 200)
		if job["status"] == "completed" || job["status"] == "failed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if job["status"] != "completed" {
		t.Fatalf("export did not complete: %+v", job)
	}
	b.call("POST", jobPath+"/cancel", map[string]any{}, 409)
	mp4 := b.mediaGet("/api/v1"+jobPath+"/content", "", 200)
	reportRaw := b.mediaGet("/api/v1"+jobPath+"/report", "", 200)
	var report map[string]any
	if json.Unmarshal(reportRaw, &report) != nil {
		t.Fatal("report is not JSON")
	}
	hash := sha256.Sum256(mp4)
	if report["mp4Sha256"] != hex.EncodeToString(hash[:]) {
		t.Fatal("export integrity report mismatch")
	}
	if report["requestor"] != user || report["displayTimezone"] != "America/Sao_Paulo" || report["createdAt"] == "" {
		t.Fatal("export provenance omitted requestor, time or zone")
	}
	decode = exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-f", "null", "-")
	decode.Stdin = bytes.NewReader(mp4)
	if output, err := decode.CombinedOutput(); err != nil {
		t.Fatalf("export not playable: %v %s", err, output)
	}
	b.call("PUT", base+"/groups/"+group, map[string]any{"name": "Somente reprodução", "revision": 2, "grants": []app.Grant{{Action: "playback", Scope: "site", ResourceID: site}}}, 200)
	b.mediaGet("/api/v1"+jobPath+"/content", "", 403)
	b.mediaGet("/api/v1"+jobPath+"/report", "", 403)
	other := b.call("POST", "/admin/tenants", map[string]string{"name": "Outro ambiente"}, 201)["id"].(string)
	b.selectTenant(other)
	b.mediaGet(session["manifest"].(string), "", 404)
	b.selectTenant(tenant)
	// Old context cannot be reused after switching, even by the same global admin.
	b.mediaGet(part, "", 410)
	policy := b.call("GET", base+"/recording-policy", nil, 200)["policy"].(map[string]any)
	reduction := b.call("POST", base+"/quota-previews", map[string]any{"targetBytes": 0, "revision": policy["revision"]}, 201)
	if reduction["result"].(map[string]any)["toFree"].(float64) <= 0 {
		t.Fatal("reduction omitted existing archive impact")
	}
	quotaRequest := map[string]any{"previewId": reduction["id"], "idempotencyKey": "quota-integration-0001"}
	b.call("POST", base+"/quota-jobs", quotaRequest, 202)
	b.call("POST", base+"/quota-jobs", quotaRequest, 202)
	deadline = time.Now().Add(8 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		list := b.call("GET", base+"/quota-jobs", nil, 200)["items"].([]any)
		if len(list) > 0 && list[0].(map[string]any)["status"] == "completed" {
			completed = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !completed {
		t.Fatal("quota reduction did not complete")
	}
	reduced := b.call("GET", base+"/recording-policy", nil, 200)
	if reduced["policy"].(map[string]any)["quotaBytes"] != float64(0) || reduced["usage"].(map[string]any)["recording"] != float64(0) {
		t.Fatal("quota applied before actual release")
	}
}
func (b *browserClient) mediaGet(path, byteRange string, status int) []byte {
	b.t.Helper()
	req, _ := http.NewRequest("GET", b.server+path, nil)
	req.Header.Set("X-VMS-Context", b.view)
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != status {
		b.t.Fatalf("media GET expected %d got %d: %v %s", status, res.StatusCode, err, raw)
	}
	return raw
}
