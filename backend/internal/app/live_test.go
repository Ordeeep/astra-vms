package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pion/webrtc/v4"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
)

func TestPublicLiveGatewayCutsActualVideoAfterGrantRevocation(t *testing.T) {
	testPublicLiveGatewayCutsActualVideoAfterGrantRevocation(t, "h264")
}

func TestPublicHEVCLiveGatewayAdaptsAndCutsVideoAfterGrantRevocation(t *testing.T) {
	testPublicLiveGatewayCutsActualVideoAfterGrantRevocation(t, "hevc")
}

func testPublicLiveGatewayCutsActualVideoAfterGrantRevocation(t *testing.T, codec string) {
	engine := os.Getenv("VMS_TEST_MEDIAMTX")
	if engine == "" || os.Getenv("VMS_TEST_DB") == "" {
		t.Skip("real PostgreSQL and media engine required")
	}
	binary, err := filepath.Abs("../../bin/vms-worker.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(binary); err != nil {
		t.Skip("build bin/vms-worker.exe before this integrated test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := filepath.Join(t.TempDir(), "source.json")
	os.WriteFile(fixture, []byte(`{"logLevel":"error","rtspAddress":"127.0.0.1:27554","rtspTransports":["tcp"],"rtmp":false,"hls":false,"srt":false,"moq":false,"webrtc":false,"paths":{"camera":{"source":"publisher"}}}`), 0600)
	sourceCmd := exec.CommandContext(ctx, engine, fixture)
	sourceProc, err := media.StartOwned(sourceCmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { sourceProc.Stop(); sourceCmd.Wait() }()
	secret := "local+integrated/test+secret=with-32-bytes"
	workerCmd := exec.CommandContext(ctx, binary, "--mediamtx", engine, "--listen", "127.0.0.1:29091", "--media-api", "127.0.0.1:29997", "--whep", "127.0.0.1:28889", "--rtsp", "127.0.0.1:28554", "--udp", "127.0.0.1:28189", "--data", t.TempDir())
	workerCmd.Env = append(os.Environ(), "VMS_WORKER_SECRET="+secret)
	workerProc, err := media.StartOwned(workerCmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { workerProc.Stop(); workerCmd.Wait() }()
	time.Sleep(700 * time.Millisecond)
	args := []string{"-hide_banner", "-v", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-an"}
	if codec == "hevc" {
		args = append(args, "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=error:pools=1:frame-threads=1:keyint=10:min-keyint=10:scenecut=0", "-g", "10", "-bf", "2")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "10", "-bf", "0")
	}
	args = append(args, "-rtsp_transport", "tcp", "-f", "rtsp", "rtsp://127.0.0.1:27554/camera")
	pub := exec.CommandContext(ctx, "ffmpeg", args...)
	publisher, err := media.StartOwned(pub)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { publisher.Stop(); pub.Wait() }()
	b := newTestAPI(t, func(o *app.Options) {
		o.WorkerURL = "http://127.0.0.1:29091"
		o.WorkerSecret = secret
		o.WHEPURL = "http://127.0.0.1:28889"
	})
	uid := b.login("admin", "admin")
	me := b.call("GET", "/me", nil, 200)
	tenant := me["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Laboratório", "address": "", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Padrão sintético", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://127.0.0.1:27554/camera"}}}, 201)["id"].(string)
	job := b.call("POST", base+"/cameras/"+camera+"/profile-tests", map[string]string{"profile": "main"}, 202)["id"].(string)
	probeDeadline := time.Now().Add(12 * time.Second)
	var diagnostic map[string]any
	for time.Now().Before(probeDeadline) {
		diagnostic = b.call("GET", base+"/jobs/"+job, nil, 200)
		if diagnostic["status"] != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if diagnostic["status"] != "completed" {
		t.Fatalf("real source diagnostic did not complete: %#v", diagnostic)
	}
	video := diagnostic["result"].(map[string]any)["video"].(map[string]any)
	if video["codec"] != codec || video["h264Candidate"] != (codec == "h264") {
		t.Fatal("synthetic source codec misclassified")
	}
	s := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": camera, "profile": "sub2", "fallback": []string{"main"}}, 201)
	if s["profile"] != "main" || s["fallback"] != true {
		t.Fatal("live fallback was not explicit")
	}
	sid := s["id"].(string)
	var ended atomic.Bool
	renewalCtx, stopRenewal := context.WithCancel(ctx)
	defer stopRenewal()
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-renewalCtx.Done():
				return
			case <-ticker.C:
				req, _ := http.NewRequestWithContext(renewalCtx, "PUT", b.server+"/api/v1"+base+"/live-sessions/"+sid, strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-CSRF-Token", b.csrf)
				req.Header.Set("X-VMS-Context", b.view)
				res, e := b.client.Do(req)
				if e != nil {
					return
				}
				res.Body.Close()
				if res.StatusCode != 200 {
					ended.Store(true)
					return
				}
			}
		}
	}()
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	var packets atomic.Int64
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, e := track.ReadRTP(); e != nil {
				return
			}
			packets.Add(1)
		}
	})
	offer, _ := pc.CreateOffer(nil)
	gather := webrtc.GatheringCompletePromise(pc)
	pc.SetLocalDescription(offer)
	<-gather
	req, _ := http.NewRequestWithContext(ctx, "POST", b.server+s["signaling"].(string), strings.NewReader(pc.LocalDescription().SDP))
	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("X-CSRF-Token", b.csrf)
	req.Header.Set("X-VMS-Context", b.view)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("gateway WHEP %d %s", res.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("worker credential exposed")
	}
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(raw)}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for packets.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if packets.Load() < 3 {
		t.Fatal("public live path produced no RTP")
	}
	// Bootstrap membership revision is 1. Remove its complete video grants.
	b.call("PUT", base+"/operators/"+uid, map[string]any{"groups": []string{}, "revision": 1}, 200)
	started := time.Now()
	before := packets.Load()
	time.Sleep(1600 * time.Millisecond)
	after := packets.Load()
	time.Sleep(350 * time.Millisecond)
	if packets.Load() != after || !ended.Load() {
		t.Fatalf("revocation failed: before=%d after=%d current=%d renewEnded=%v elapsed=%v", before, after, packets.Load(), ended.Load(), time.Since(started))
	}
	denied := b.call("POST", base+"/live-sessions", map[string]any{"cameraId": camera, "profile": "main"}, 403)
	raw, _ = json.Marshal(denied)
	if bytes.Contains(raw, []byte("rtsp://")) {
		t.Fatal("source leaked")
	}
}
