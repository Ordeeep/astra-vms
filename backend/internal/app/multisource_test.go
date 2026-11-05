package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

// A local functional regression, not the P0 browser/network homologation.
// Eight independent publishers, two authenticated operators and four sites
// exercise shared disk/catalog admission without touching the development data.
func TestEightIndependentRTSPSourcesKeepTenantArchivesSeparate(t *testing.T) {
	engine := os.Getenv("VMS_TEST_MEDIAMTX")
	if os.Getenv("VMS_TEST_MULTISOURCE") != "1" || os.Getenv("VMS_TEST_DB") == "" || engine == "" {
		t.Skip("opt-in real multi-source integration: VMS_TEST_MULTISOURCE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	paths := map[string]any{}
	for i := range 8 {
		paths[fmt.Sprintf("camera-%d", i)] = map[string]any{"source": "publisher"}
	}
	config, err := json.Marshal(map[string]any{
		"logLevel": "error", "rtspAddress": address, "rtspTransports": []string{"tcp"},
		"rtmp": false, "hls": false, "srt": false, "moq": false, "webrtc": false,
		"authInternalUsers": []any{map[string]any{
			"user": "fixture", "pass": "fixture-only-password",
			"permissions": []any{map[string]string{"action": "publish"}, map[string]string{"action": "read"}},
		}}, "paths": paths,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "source.json")
	if err = os.WriteFile(fixture, config, 0600); err != nil {
		t.Fatal(err)
	}
	startOwned := func(command *exec.Cmd) {
		t.Helper()
		process, e := media.StartOwned(command)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { process.Stop(); command.Wait() })
	}
	startOwned(exec.CommandContext(ctx, engine, fixture))
	deadline := time.Now().Add(4 * time.Second)
	for {
		conn, e := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture RTSP listener did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	colors := []string{"red", "green", "blue", "yellow", "magenta", "cyan", "white", "gray"}
	urls := make([]string, 8)
	for i, color := range colors {
		urls[i] = fmt.Sprintf("rtsp://fixture:fixture-only-password@%s/camera-%d", address, i)
		startOwned(exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-re", "-f", "lavfi", "-i",
			"color=c="+color+":s=160x90:r=10", "-an", "-c:v", "libx264", "-threads", "1",
			"-preset", "ultrafast", "-tune", "zerolatency", "-g", "10", "-bf", "0",
			"-rtsp_transport", "tcp", "-f", "rtsp", urls[i]))
	}
	archive, err := media.NewArchiveService(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := "multi-source-private-fixture-secret"
	worker := httptest.NewServer(media.NewGuard(media.GuardOptions{ServiceSecret: secret, Archive: archive}))
	t.Cleanup(func() { worker.Close(); archive.Close() })
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = secret })
	b.login("admin", "admin")
	tenants := []string{
		b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string),
		b.call("POST", "/admin/tenants", map[string]any{"name": "Independent tenant"}, 201)["id"].(string),
	}
	b.call("POST", "/admin/storage", map[string]any{"path": t.TempDir(), "volumeLimit": 64 << 20, "margin": 1 << 20}, 200)
	type monitored struct{ tenant, site, camera string }
	cameras := []monitored{}
	operators := []*browserClient{}
	start := time.Now().UTC()
	for n, tenant := range tenants {
		b.selectTenant(tenant)
		base := "/tenants/" + tenant
		b.call("PUT", base+"/recording-policy", map[string]any{"quotaBytes": 24 << 20, "exportBytes": 4 << 20, "retentionDays": 10, "exportHours": 24, "revision": 0}, 200)
		username := fmt.Sprintf("multisource-%d", n)
		operator := b.call("POST", "/admin/operators", map[string]any{"username": username, "password": "fixture-password"}, 201)["id"].(string)
		group := b.call("POST", base+"/groups", map[string]any{"name": "Archive viewers", "grants": []app.Grant{{Action: "playback", Scope: "tenant", ResourceID: tenant}}}, 201)["id"].(string)
		b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{group}, "revision": 0}, 200)
		operators = append(operators, independentBrowser(t, b, username, "fixture-password", tenant))
		for s := range 2 {
			site := b.call("POST", base+"/sites", map[string]any{"name": fmt.Sprintf("Site %d", s), "timezone": "UTC"}, 201)["id"].(string)
			for c := range 2 {
				i := n*4 + s*2 + c
				camera := b.call("POST", base+"/cameras", map[string]any{"name": colors[i], "siteId": site,
					"profiles": []map[string]string{{"id": "main", "name": "Main", "url": urls[i]}}}, 201)["id"].(string)
				b.call("PUT", base+"/cameras/"+camera+"/recording", map[string]any{"enabled": true, "profile": "main", "schedule": []any{}, "always": true, "revision": 0}, 200)
				cameras = append(cameras, monitored{tenant, site, camera})
			}
		}
	}
	time.Sleep(6 * time.Second)
	for _, tenant := range tenants {
		b.selectTenant(tenant)
		for _, camera := range cameras {
			if camera.tenant == tenant {
				b.call("PUT", "/tenants/"+tenant+"/cameras/"+camera.camera+"/recording", map[string]any{"enabled": false, "profile": "main", "schedule": []any{}, "always": true, "revision": 1}, 200)
			}
		}
	}
	end := time.Now().UTC()
	query := "?start=" + url.QueryEscape(start.Format(time.RFC3339Nano)) + "&end=" + url.QueryEscape(end.Format(time.RFC3339Nano))
	expected := [][3]int{{255, 0, 0}, {0, 128, 0}, {0, 0, 255}, {255, 255, 0}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}, {128, 128, 128}}
	for i, camera := range cameras {
		op := operators[i/4]
		base := "/tenants/" + camera.tenant
		var items []any
		deadline = time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			items = op.call("GET", base+"/cameras/"+camera.camera+"/timeline"+query, nil, 200)["items"].([]any)
			if len(items) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if len(items) == 0 {
			t.Fatalf("camera %d has no recording", i)
		}
		first := items[0].(map[string]any)
		if first["siteId"] != camera.site {
			t.Fatal("archive lost its site")
		}
		session := op.call("POST", base+"/playback-sessions", map[string]any{"cameraId": camera.camera, "start": first["start"], "end": end}, 201)
		playlist := string(op.mediaGet(session["manifest"].(string), "", 200))
		var init, part string
		for _, line := range strings.Split(playlist, "\n") {
			if strings.HasPrefix(line, "#EXT-X-MAP:") && init == "" {
				init = strings.Split(line, "\"")[1]
			}
			if strings.HasPrefix(line, "/api/") && part == "" {
				part = line
			}
		}
		data := append(op.mediaGet(init, "", 200), op.mediaGet(part, "", 200)...)
		decode := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1")
		decode.Stdin = bytes.NewReader(data)
		frame, e := decode.Output()
		if e != nil || len(frame) != 160*90*3 {
			t.Fatalf("camera %d decode: %v, bytes=%d", i, e, len(frame))
		}
		for channel := range 3 {
			sum := 0
			for p := channel; p < len(frame); p += 3 {
				sum += int(frame[p])
			}
			average := sum / (160 * 90)
			if delta := average - expected[i][channel]; delta < -8 || delta > 8 {
				t.Fatalf("camera %d showed another source: channel=%d actual=%d", i, channel, average)
			}
		}
		other := operators[1-i/4]
		other.call("GET", base+"/cameras/"+camera.camera+"/timeline"+query, nil, 404)
		other.mediaGet(part, "", 404)
		op.call("DELETE", base+"/playback-sessions/"+session["id"].(string), nil, 204)
	}
	t.Log("Eight authenticated independent sources decoded with expected colors; two tenant contexts rejected foreign catalog and media.")
}
