package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"vms.local/backend/internal/media"

	"github.com/pion/webrtc/v4"
)

func TestRealWebRTCSessionIsCutOnRevokeAndLeaseExpiry(t *testing.T) {
	engine := os.Getenv("VMS_TEST_MEDIAMTX")
	if engine == "" {
		t.Skip("set VMS_TEST_MEDIAMTX to run real engine test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	guard := media.NewGuard(media.GuardOptions{ControlURL: "http://127.0.0.1:22997", ServiceSecret: "test-service-only", OnControlFailure: func() { cancel() }})
	guard.AllowSource("tenant-a-camera")
	auth := httptest.NewServer(guard)
	defer auth.Close()
	go guard.Run(ctx)
	config := fmt.Sprintf(`{"logLevel":"debug","authMethod":"http","authHTTPAddress":"%s/auth?key=test-service-only","authHTTPExclude":[],"api":true,"apiAddress":"127.0.0.1:22997","rtspAddress":"127.0.0.1:22554","rtspTransports":["tcp"],"rtmp":false,"hls":false,"srt":false,"moq":false,"webrtc":true,"webrtcAddress":"127.0.0.1:22889","webrtcLocalUDPAddress":"127.0.0.1:22890","webrtcIPsFromInterfaces":false,"webrtcAdditionalHosts":["127.0.0.1"],"paths":{"tenant-a-camera":{"source":"publisher"}}}`, auth.URL)
	path := filepath.Join(t.TempDir(), "mediamtx.json")
	os.WriteFile(path, []byte(config), 0600)
	engineCmd := exec.CommandContext(ctx, engine, path)
	var engineLog bytes.Buffer
	engineCmd.Stdout = &engineLog
	engineCmd.Stderr = &engineLog
	if err := engineCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		engineCmd.Process.Kill()
		engineCmd.Wait()
		if t.Failed() {
			t.Log(engineLog.String())
		}
	}()
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:22997/v3/paths/list", nil)
		req.SetBasicAuth("service", "test-service-only")
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	pub := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "10", "-bf", "0", "-rtsp_transport", "tcp", "-f", "rtsp", "rtsp://service:test-service-only@127.0.0.1:22554/tenant-a-camera")
	if err := pub.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { pub.Process.Kill(); pub.Wait() }()
	time.Sleep(time.Second)
	var packets atomic.Int64
	openPeer := func(token string) *webrtc.PeerConnection {
		pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(func() webrtc.SettingEngine {
			s := webrtc.SettingEngine{}
			s.SetIncludeLoopbackCandidate(true)
			return s
		}())).NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
		pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				if _, _, err := track.ReadRTP(); err != nil {
					return
				}
				packets.Add(1)
			}
		})
		offer, _ := pc.CreateOffer(nil)
		gather := webrtc.GatheringCompletePromise(pc)
		pc.SetLocalDescription(offer)
		<-gather
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:22889/tenant-a-camera/whep", strings.NewReader(pc.LocalDescription().SDP))
		req.Header.Set("Content-Type", "application/sdp")
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			pc.Close()
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 201 {
			pc.Close()
			t.Fatalf("WHEP %d %s", res.StatusCode, body)
		}
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(body)}); err != nil {
			t.Fatal(err)
		}
		return pc
	}
	lease, err := guard.Grant("tenant-a", "context-a", "tenant-a-camera", 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// The immutable path in the grant must reject a valid token used elsewhere.
	payload, _ := json.Marshal(map[string]string{"action": "read", "protocol": "webrtc", "token": lease.Token, "path": "tenant-b-camera", "id": "foreign"})
	req, _ := http.NewRequest("POST", auth.URL+"/auth?key=test-service-only", bytes.NewReader(payload))
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("cross path status %d", res.StatusCode)
	}
	pc := openPeer(lease.Token)
	defer pc.Close()
	for i := 0; i < 20 && packets.Load() < 2; i++ {
		guard.Renew(lease.Token, 1500*time.Millisecond)
		time.Sleep(100 * time.Millisecond)
	}
	if packets.Load() < 2 {
		t.Fatal("no real RTP received")
	}
	start := time.Now()
	guard.Revoke(lease.Token)
	time.Sleep(500 * time.Millisecond)
	after := packets.Load()
	time.Sleep(500 * time.Millisecond)
	if packets.Load() != after || time.Since(start) > 2*time.Second {
		t.Fatal("RTP continued after revocation")
	}
	second, _ := guard.Grant("tenant-a", "context-a", "tenant-a-camera", 1200*time.Millisecond)
	pc2 := openPeer(second.Token)
	defer pc2.Close()
	before := packets.Load()
	time.Sleep(700 * time.Millisecond)
	if packets.Load() == before {
		t.Fatal("second session did not receive")
	}
	// No renewals simulates loss of the core's authority channel.
	time.Sleep(900 * time.Millisecond)
	after = packets.Load()
	time.Sleep(350 * time.Millisecond)
	if packets.Load() != after {
		t.Fatal("RTP continued after authority lease expired")
	}
}
