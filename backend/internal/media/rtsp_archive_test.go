package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"vms.local/backend/internal/media"
)

func TestNativeRTSPArchivePreservesFrameTimeAndDecodes(t *testing.T) {
	source := os.Getenv("VMS_TEST_RTSP")
	if source == "" {
		t.Skip("set VMS_TEST_RTSP to a synthetic H264 source")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	parts := []media.ArchivePart{}
	err := media.ReceiveArchive(ctx, source, func(p media.ArchivePart) error {
		parts = append(parts, p)
		if len(parts) == 3 {
			cancel()
		}
		return nil
	}, nil)
	if len(parts) != 3 {
		t.Fatalf("parts=%d: %v", len(parts), err)
	}
	for i, p := range parts {
		var ticks uint64
		for _, d := range p.Durations {
			ticks += uint64(d)
		}
		expected := time.Duration(ticks) * time.Second / 90000
		if delta := p.End.Sub(p.Start) - expected; delta > time.Microsecond || delta < -time.Microsecond {
			t.Fatalf("duration mismatch %v", delta)
		}
		if i > 0 && !p.Start.Equal(parts[i-1].End) {
			t.Fatal("unexpected epoch gap")
		}
		data := append(bytes.Clone(p.Init), p.Data...)
		probe := exec.Command("ffprobe", "-v", "error", "-count_frames", "-show_entries", "stream=nb_read_frames,duration", "-of", "json", "pipe:0")
		probe.Stdin = bytes.NewReader(data)
		raw, e := probe.Output()
		if e != nil {
			t.Fatal(e)
		}
		var result struct {
			Streams []struct {
				Frames   string `json:"nb_read_frames"`
				Duration string `json:"duration"`
			}
		}
		if json.Unmarshal(raw, &result) != nil || len(result.Streams) != 1 {
			t.Fatalf("invalid probe %s", raw)
		}
		frames, _ := strconv.Atoi(result.Streams[0].Frames)
		duration, _ := strconv.ParseFloat(result.Streams[0].Duration, 64)
		if frames != p.Frames || duration < 0.9 || duration > 1.1 {
			t.Fatalf("frames/time mismatch %s %+v", raw, p)
		}
		decode := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", "pipe:0", "-f", "null", "-")
		decode.Stdin = bytes.NewReader(data)
		if out, e := decode.CombinedOutput(); e != nil {
			t.Fatalf("decode %v %s", e, out)
		}
	}
}
