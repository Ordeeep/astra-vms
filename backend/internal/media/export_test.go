package media_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"

	"vms.local/backend/internal/media"
)

func TestExportPreservesElapsedTimeBlackGapsAndFrameCuts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	epoch := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	sources := map[string][]byte{}
	clips := []media.ExportClip{}
	for i, id := range []string{"a", "b"} {
		// Each source frame has a different known luminance, independently of
		// the export implementation's timestamp mapping.
		base := 20 + i*80
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("nullsrc=s=64x48:r=10,geq=lum='%d+N*5':cb=128:cr=128", base), "-frames:v", "20", "-c:v", "libx264", "-crf", "0", "-bf", "0", "-g", "10", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
		raw, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		sources[id] = raw
		start := epoch.Add(time.Duration(i*3) * time.Second)
		durations := make([]uint32, 20)
		for n := range durations {
			durations[n] = 9000
		}
		clips = append(clips, media.ExportClip{ID: id, Index: media.SegmentIndex{Start: start, End: start.Add(2 * time.Second), Parts: []media.PartIndex{{ArchivePart: media.ArchivePart{Start: start, End: start.Add(2 * time.Second), Timescale: 90000, Width: 64, Height: 48, Frames: 20, Durations: durations}}}}})
	}
	plan := media.ExportPlan{Start: epoch.Add(350 * time.Millisecond), End: epoch.Add(4550 * time.Millisecond), Width: 64, Height: 48, FPS: 10, Clips: clips}
	var output bytes.Buffer
	err := media.RenderExport(ctx, "ffmpeg", plan, func(_ context.Context, clip media.ExportClip) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(sources[clip.ID])), nil
	}, &output, nil)
	if err != nil {
		t.Fatal(err)
	}
	decode := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-f", "rawvideo", "-pix_fmt", "yuv420p", "pipe:1")
	decode.Stdin = bytes.NewReader(output.Bytes())
	frames, err := decode.Output()
	if err != nil {
		t.Fatal(err)
	}
	frameSize := 64 * 48 * 3 / 2
	if len(frames) != 42*frameSize {
		t.Fatalf("expected 42 frames (4.2 s), got %d bytes", len(frames))
	}
	for n := 0; n < 42; n++ {
		instant := .35 + float64(n)/10
		expected := 16
		if instant < 2 {
			expected = 20 + int(instant*10)*5
		}
		if instant >= 3 {
			expected = 100 + int((instant-3)*10+1e-6)*5
		}
		actual := int(frames[n*frameSize+64*24+32])
		if actual < expected-2 || actual > expected+2 {
			t.Fatalf("frame %d UTC offset %.2f: luma=%d expected=%d", n, instant, actual, expected)
		}
	}
}
