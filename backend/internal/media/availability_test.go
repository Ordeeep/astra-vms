package media

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type availabilityRTSP struct {
	stream    *gortsplib.ServerStream
	describes atomic.Int32
}

func (s *availabilityRTSP) OnDescribe(c *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	s.describes.Add(1)
	if strings.Trim(c.Path, "/") == "denied" {
		return &base.Response{StatusCode: base.StatusForbidden}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, s.stream, nil
}
func (s *availabilityRTSP) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, s.stream, nil
}
func (s *availabilityRTSP) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}
func syntheticAvailabilitySource(t *testing.T, hevc, parametersOnly bool) (string, *availabilityRTSP) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &availabilityRTSP{}
	server := &gortsplib.Server{RTSPAddress: listener.Addr().String(), Handler: h, Listen: func(string, string) (net.Listener, error) { return listener, nil }}
	if err = server.Start(); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	var codec format.Format = &format.H264{PayloadTyp: 96, PacketizationMode: 1}
	payload := []byte{0x65, 0x88, 0x84}
	if parametersOnly {
		payload = []byte{0x67, 0x42, 0, 0x1f}
	}
	if hevc {
		codec = &format.H265{PayloadTyp: 96}
		payload = []byte{0x26, 0x01, 0x88}
		if parametersOnly {
			payload = []byte{0x40, 0x01, 0x88}
		}
	}
	medi := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{codec}}
	h.stream = &gortsplib.ServerStream{Server: server, Desc: &description.Session{Medias: []*description.Media{medi}}}
	if err = h.stream.Initialize(); err != nil {
		server.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var sequence uint16
		var timestamp uint32
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sequence++
				timestamp += 1800
				h.stream.WritePacketRTP(medi, &rtp.Packet{Header: rtp.Header{Version: 2, Marker: true, PayloadType: 96, SequenceNumber: sequence, Timestamp: timestamp, SSRC: 12345}, Payload: payload})
			}
		}
	}()
	t.Cleanup(func() { close(done); <-stopped; h.stream.Close(); server.Close() })
	return "rtsp://" + listener.Addr().String() + "/video", h
}
func TestAvailabilityRequiresPictureForH264AndH265(t *testing.T) {
	for _, hevc := range []bool{false, true} {
		for _, metadata := range []bool{false, true} {
			name := "H264"
			if hevc {
				name = "H265"
			}
			if metadata {
				name += "_parameters_only"
			}
			t.Run(name, func(t *testing.T) {
				address, _ := syntheticAvailabilitySource(t, hevc, metadata)
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				at, code := ProbeVideo(ctx, address)
				if metadata {
					if code == "" || !at.IsZero() {
						t.Fatal("parameter sets are not video pictures")
					}
				} else if code != "" || at.IsZero() {
					t.Fatalf("video not confirmed: %s", code)
				}
			})
		}
	}
}
func TestAvailabilityMetadataAndAuthenticationFailures(t *testing.T) {
	address, _ := syntheticAvailabilitySource(t, false, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, code := ProbeVideo(ctx, strings.TrimSuffix(address, "video")+"denied"); code != "authentication_failed" {
		t.Fatalf("expected safe auth diagnostic, got %s", code)
	}
}
func availabilityWorkerRequest(t *testing.T, g *Guard, request AvailabilityRequest, ctx context.Context) AvailabilityResult {
	t.Helper()
	raw, _ := json.Marshal(request)
	r := httptest.NewRequest("POST", "/availability", bytes.NewReader(raw)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("worker status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "rtsp://") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("worker exposed connection details")
	}
	var result AvailabilityResult
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("bad result")
	}
	return result
}
func TestAvailabilityAnyProfileAndArchiveReuse(t *testing.T) {
	address, _ := syntheticAvailabilitySource(t, false, false)
	g := NewGuard(GuardOptions{ServiceSecret: "test"})
	defer g.cancel()
	req := AvailabilityRequest{Tenant: "tenant", Camera: "camera", Source: "source", ConnectionRevision: 3, Profiles: []AvailabilityProfile{{ID: "main", URL: strings.TrimSuffix(address, "video") + "denied"}, {ID: "sub1", URL: address}}}
	result := availabilityWorkerRequest(t, g, req, context.Background())
	positive := false
	for _, p := range result.Profiles {
		positive = positive || p.Available
	}
	if !positive {
		t.Fatal("working substream must confirm availability")
	}
	now := time.Now().UTC()
	plan := CapturePlan{ID: "capture", Tenant: "tenant", Camera: "camera", Source: "source", ConnectionRevision: 3, Profile: "main", URL: "rtsp://127.0.0.1:1/secret", Expires: now.Add(time.Second)}
	archive := &ArchiveService{captures: map[string]*capture{"capture": {plan: plan, status: CaptureStatus{LastFrame: now}}}}
	g.opts.Archive = archive
	req.Profiles = []AvailabilityProfile{{ID: "main", URL: plan.URL}}
	reused := availabilityWorkerRequest(t, g, req, context.Background())
	if len(reused.Profiles) != 1 || !reused.Profiles[0].Available {
		t.Fatal("fresh matching capture must avoid source access")
	}
	req.ConnectionRevision++
	if _, ok := archive.recentVideo(req, req.Profiles[0], now); ok {
		t.Fatal("must not reuse previous connection revision")
	}
	req.ConnectionRevision--
	req.Tenant = "other"
	if _, ok := archive.recentVideo(req, req.Profiles[0], now); ok {
		t.Fatal("must not reuse another tenant")
	}
	req.Tenant = "tenant"
	if _, ok := archive.recentVideo(req, req.Profiles[0], now.Add(4*time.Second)); ok {
		t.Fatal("stale or expired capture evidence")
	}
}
func TestAvailabilityReusesLocalLiveAndBoundsCapacity(t *testing.T) {
	address, _ := syntheticAvailabilitySource(t, false, false)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{\"ready\":true}")) }))
	defer control.Close()
	local := strings.TrimSuffix(strings.TrimPrefix(address, "rtsp://"), "/video")
	g := NewGuard(GuardOptions{ServiceSecret: "test", ControlURL: control.URL, RTSPAddress: local})
	defer g.cancel()
	p := AvailabilityProfile{ID: "main", URL: "rtsp://127.0.0.1:1/private", Paths: []string{"current_native"}}
	g.configured["current_native"] = p.URL
	req := AvailabilityRequest{Tenant: "tenant", Camera: "camera", Source: "source", Profiles: []AvailabilityProfile{p}}
	result := availabilityWorkerRequest(t, g, req, context.Background())
	if !result.Profiles[0].Available {
		t.Fatal("local live picture must avoid unreachable original source")
	}
	g.configured = map[string]string{}
	p.Paths = nil
	req.Profiles = []AvailabilityProfile{p}
	for i := 0; i < cap(g.availabilitySlots); i++ {
		g.availabilitySlots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	result = availabilityWorkerRequest(t, g, req, ctx)
	if len(result.Profiles) != 1 || result.Profiles[0].Complete || result.Profiles[0].Available {
		t.Fatal("capacity exhaustion is inconclusive, not offline")
	}
	if len(g.availabilitySlots) != cap(g.availabilitySlots) {
		t.Fatal("must not release another task's slot")
	}
}
func TestAvailabilityWorkerRequiresPrivateAuthentication(t *testing.T) {
	g := NewGuard(GuardOptions{ServiceSecret: "test"})
	defer g.cancel()
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("POST", "/availability", strings.NewReader("{}")))
	if w.Code != 401 {
		t.Fatal("availability probes must remain private")
	}
}
