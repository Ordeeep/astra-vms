package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vms.local/backend/internal/storage"
)

func TestHEVCPartConvertsToBrowserH264(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x96:rate=10", "-frames:v", "20", "-an", "-c:v", "libx265", "-threads", "2", "-x265-params", "pools=1:frame-threads=1:bframes=2:b-adapt=0:open-gop=0:keyint=20:min-keyint=20:log-level=error", "-tag:v", "hvc1", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	input, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	part, e := TranscodeMP4(ctx, "ffmpeg", input)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(part.Codec, "avc1.") {
		t.Fatal(part.Codec)
	}
	assertMP4(t, ctx, append(bytes.Clone(part.Init), part.Data...), "h264", 20)
}
func assertMP4(t *testing.T, ctx context.Context, input []byte, codec string, frames int, allowReordered ...bool) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=codec_name,nb_read_frames,has_b_frames", "-of", "json", "pipe:0")
	cmd.Stdin = bytes.NewReader(input)
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	var result struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Frames  string `json:"nb_read_frames"`
			BFrames int    `json:"has_b_frames"`
		}
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Streams) != 1 || result.Streams[0].Codec != codec || result.Streams[0].Frames != strconv.Itoa(frames) || (codec == "h264" && result.Streams[0].BFrames != 0 && (len(allowReordered) == 0 || !allowReordered[0])) {
		t.Fatalf("unexpected media %s", raw)
	}
	decode := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-f", "null", "-")
	decode.Stdin = bytes.NewReader(input)
	if raw, e = decode.CombinedOutput(); e != nil {
		t.Fatalf("decode %v %s", e, raw)
	}
}
func freeMediaAddress(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestRealHEVCArchiveBrowserLiveAndCompatibleObject(t *testing.T) {
	testRealReorderedArchive(t, "hevc")
}
func TestRealH264BFrameArchiveBrowserLiveAndCompatibleObject(t *testing.T) {
	testRealReorderedArchive(t, "h264")
}
func testRealReorderedArchive(t *testing.T, nativeCodec string) {
	engine := os.Getenv("VMS_TEST_MEDIAMTX")
	if engine == "" {
		t.Skip("set VMS_TEST_MEDIAMTX for real RTSP test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	api, rtsp := freeMediaAddress(t), freeMediaAddress(t)
	guard := NewGuard(GuardOptions{ControlURL: "http://" + api, RTSPAddress: rtsp, ServiceSecret: "synthetic-only-secret"})
	guard.AllowSource("original")
	auth := httptest.NewServer(guard)
	defer auth.Close()
	go guard.Run(ctx)
	config := map[string]any{"logLevel": "error", "authMethod": "http", "authHTTPAddress": auth.URL + "/auth?key=synthetic-only-secret", "authHTTPExclude": []any{}, "api": true, "apiAddress": api, "rtspAddress": rtsp, "rtspTransports": []string{"tcp"}, "rtmp": false, "hls": false, "srt": false, "moq": false, "webrtc": false, "paths": map[string]any{"original": map[string]any{"source": "publisher"}}}
	raw, _ := json.Marshal(config)
	file := filepath.Join(t.TempDir(), "mediamtx.json")
	os.WriteFile(file, raw, 0600)
	motor := exec.CommandContext(ctx, engine, file)
	if e := motor.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { motor.Process.Kill(); motor.Wait() }()
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+api+"/v3/paths/list", nil)
		req.SetBasicAuth("service", "synthetic-only-secret")
		res, e := http.DefaultClient.Do(req)
		if e == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	address := "rtsp://service:synthetic-only-secret@" + rtsp + "/original"
	args := []string{"-v", "error", "-re", "-f", "lavfi", "-i", "testsrc2=size=160x96:rate=10", "-an"}
	if nativeCodec == "hevc" {
		args = append(args, "-c:v", "libx265", "-threads", "2", "-x265-params", "pools=1:frame-threads=1:bframes=2:b-adapt=0:open-gop=0:keyint=10:min-keyint=10:log-level=error")
	} else {
		args = append(args, "-c:v", "libx264", "-threads", "2", "-bf", "2", "-g", "10", "-keyint_min", "10", "-sc_threshold", "0")
	}
	args = append(args, "-rtsp_transport", "tcp", "-f", "rtsp", address)
	publisher := exec.CommandContext(ctx, "ffmpeg", args...)

	publisher.Stderr = io.Discard
	if e := publisher.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { publisher.Process.Kill(); publisher.Wait() }()
	for i := 0; i < 50 && !guard.pathReady(ctx, "original"); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	captureCtx, stopCapture := context.WithTimeout(ctx, 12*time.Second)
	defer stopCapture()
	var parts []ArchivePart
	err := ReceiveArchive(captureCtx, address, func(p ArchivePart) error {
		parts = append(parts, p)
		if len(parts) == 2 {
			stopCapture()
		}
		return nil
	}, nil)
	if len(parts) != 2 {
		t.Fatalf("capture %d parts: %v", len(parts), err)
	}
	for i, p := range parts {
		prefix := "hvc1."
		if nativeCodec == "h264" {
			prefix = "avc1."
		}
		if !strings.HasPrefix(p.Codec, prefix) {
			t.Fatal(p.Codec)
		}
		if p.Frames != 10 || p.End.Sub(p.Start) != time.Second {
			t.Fatalf("unexpected part frames/time: %d %s", p.Frames, p.End.Sub(p.Start))
		}
		if i > 0 && !p.Start.Equal(parts[i-1].End) {
			t.Fatal("unexpected recording gap")
		}
		assertMP4(t, ctx, append(bytes.Clone(p.Init), p.Data...), nativeCodec, p.Frames, true)
	}
	// A failed spawn must remove its private path so a later retry can succeed.
	guard.opts.FFmpeg = filepath.Join(t.TempDir(), "missing-ffmpeg.exe")
	if err = guard.SetSourceMode(ctx, "tenant_retry_web", address, true, true); err == nil {
		t.Fatal("expected unavailable encoder")
	}
	guard.opts.FFmpeg = "ffmpeg"
	if err = guard.SetSourceMode(ctx, "tenant_retry_web", address, true, true); err != nil {
		t.Fatalf("startup retry failed: %v", err)
	}
	guard.sourceMu.Lock()
	retry := guard.converters["tenant_retry_web"]
	retry.touched = time.Now().Add(-4 * time.Second)
	guard.sourceMu.Unlock()
	guard.cleanupConverters(time.Now())
	if err = guard.SetSourceMode(ctx, "tenant_source_main_web", address, true, false); err != nil {
		t.Fatal(err)
	}
	lease, e := guard.Grant("tenant-a", "context", "tenant_source_main_web", 1500*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	renewCtx, stopRenew := context.WithCancel(ctx)
	defer stopRenew()
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				guard.Renew(lease.Token, 1500*time.Millisecond)
			}
		}
	}()
	result, e := Probe(ctx, "rtsp://service:synthetic-only-secret@"+rtsp+"/tenant_source_main_web")
	if e != nil || result.Codec != "h264" || result.BFrames != 0 {
		t.Fatalf("browser live %+v %v", result, e)
	}
	// A new reader reuses the same converter, and lease loss retires it.
	guard.sourceMu.Lock()
	converter := guard.converters["tenant_source_main_web"]
	guard.sourceMu.Unlock()
	if err = guard.SetSourceMode(ctx, "tenant_source_main_web", address, true, false); err != nil {
		t.Fatal("converter not shared")
	}
	stopRenew()
	guard.Revoke(lease.Token)
	guard.sourceMu.Lock()
	converter.touched = time.Now().Add(-4 * time.Second)
	guard.sourceMu.Unlock()
	guard.cleanupConverters(time.Now())
	select {
	case <-converter.done:
	case <-time.After(3 * time.Second):
		t.Fatal("orphan converter")
	}

	archive, e := NewArchiveService(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	if e = archive.configure(ArchiveConfig{Path: t.TempDir(), VolumeLimit: 4 << 20}); e != nil {
		t.Fatal(e)
	}
	if e = archive.store.SetLimit("tenant-a", storage.Recording, 2<<20); e != nil {
		t.Fatal(e)
	}
	part := parts[0]
	writer, e := archive.store.CreateCapture("tenant-a", int64(len(part.Init)+len(part.Data)))
	if e != nil {
		t.Fatal(e)
	}
	writer.Write(part.Init)
	writer.Write(part.Data)
	index := SegmentIndex{Tenant: "tenant-a", Camera: "camera", Source: "source", Start: part.Start, End: part.End, InitLength: int64(len(part.Init)), Parts: []PartIndex{{ArchivePart: part, Offset: int64(len(part.Init)), Length: int64(len(part.Data))}}}
	meta, _ := json.Marshal(index)
	if e = writer.FinishWithMetadata(meta); e != nil {
		t.Fatal(e)
	}
	var converted []byte
	for _, kind := range []string{"init", "data"} {
		req := httptest.NewRequest("GET", fmt.Sprintf("/archive/objects/%s/compatible?tenant=tenant-a&part=0&kind=%s", writer.ID(), kind), nil)
		res := httptest.NewRecorder()
		archive.ServeHTTP(res, req)
		if res.Code != 200 || res.Header().Get("Content-Length") == "" {
			t.Fatalf("compatible %d %s", res.Code, res.Body.String())
		}
		converted = append(converted, res.Body.Bytes()...)
	}
	assertMP4(t, ctx, converted, "h264", 10)
	req := httptest.NewRequest("GET", fmt.Sprintf("/archive/objects/%s/compatible?tenant=tenant-a&part=0&kind=data", writer.ID()), nil)
	req.Header.Set("Range", "bytes=0-7")
	res := httptest.NewRecorder()
	archive.ServeHTTP(res, req)
	if res.Code != 206 || res.Body.Len() != 8 {
		t.Fatal("Range unsupported")
	}
	for _, query := range []string{"tenant=tenant-b&part=0&kind=data", "tenant=tenant-a&part=-1&kind=data", "tenant=tenant-a&part=9&kind=init"} {
		res := httptest.NewRecorder()
		archive.ServeHTTP(res, httptest.NewRequest("GET", "/archive/objects/"+writer.ID()+"/compatible?"+query, nil))
		if res.Code == 200 || res.Code == 206 {
			t.Fatalf("invalid access %s", query)
		}
	}
	// H265 source export remains H264 and preserves the planned elapsed time.
	plan := ExportPlan{Tenant: "tenant-a", Start: part.Start, End: part.End, Width: 160, Height: 96, FPS: 10, Clips: []ExportClip{{ID: writer.ID(), Index: index}}}
	var output bytes.Buffer
	e = RenderExport(ctx, "ffmpeg", plan, func(ctx context.Context, _ ExportClip) (io.ReadCloser, error) {
		r, e := archive.store.Read("tenant-a", writer.ID())
		if e != nil {
			return nil, e
		}
		return CanonicalExportInput(ctx, r, index), nil
	}, &output, nil)
	if e != nil {
		t.Fatal(e)
	}
	assertMP4(t, ctx, output.Bytes(), "h264", 10)
}
func TestSourcePreparationsAreParallelBoundedAndShared(t *testing.T) {
	arrived := make(chan string, 8)
	release := make(chan struct{})
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { arrived <- r.URL.Path; <-release; w.WriteHeader(200) }))
	defer control.Close()
	guard := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "test"})
	defer guard.closeConverters()
	guard.client.Timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func(i int) {
			results <- guard.SetSourceMode(ctx, fmt.Sprintf("path%d", i), "rtsp://127.0.0.1/example", false, false)
		}(i)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-arrived:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("independent source preparation serialized")
		}
	}
	if e := guard.SetSourceMode(ctx, "excess", "rtsp://127.0.0.1/example", false, false); e == nil {
		close(release)
		t.Fatal("preparation capacity was not bounded")
	}
	close(release)
	for i := 0; i < 4; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	// Reusing a prepared path makes no additional motor request.
	if e := guard.SetSourceMode(ctx, "path0", "rtsp://127.0.0.1/example", false, false); e != nil {
		t.Fatal(e)
	}
	select {
	case path := <-arrived:
		t.Fatalf("duplicate configuration %s", path)
	default:
	}
}
