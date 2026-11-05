package media_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"vms.local/backend/internal/media"
	"vms.local/backend/internal/storage"
)

func TestInvalidMovieBoxesAreNeverAdvertisedAsRecording(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetLimit("a", storage.Recording, 1<<20)
	emptyBoxes := []byte{0, 0, 0, 8, 'm', 'o', 'o', 'v', 0, 0, 0, 8, 'm', 'o', 'o', 'f', 0, 0, 0, 8, 'm', 'd', 'a', 't'}
	advertised := false
	err = media.RecordFragments(context.Background(), bytes.NewReader(emptyBoxes), s, "a", 1<<20, func(media.Fragment) { advertised = true })
	if err == nil || advertised {
		t.Fatal("invalid media was advertised")
	}
}

func TestFragmentedVideoRemainsDecodableAcrossQuotaOverwrite(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg integration prerequisite unavailable")
	}
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLimit("a", storage.Recording, 128<<10); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	producer := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "6", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "10", "-bf", "0", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	out, err := producer.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.Start(); err != nil {
		t.Fatal(err)
	}
	count := 0
	err = media.RecordFragments(ctx, out, s, "a", 1<<20, func(media.Fragment) { count++ })
	if err != nil {
		producer.Process.Kill()
		t.Fatal(err)
	}
	if err := producer.Wait(); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("fragments=%d want 6", count)
	}
	entries := s.Entries("a", storage.Recording)
	if len(entries) == 0 || len(entries) >= 6 {
		t.Fatalf("expected overwrite with survivors, got %d", len(entries))
	}
	for _, entry := range entries {
		r, err := s.Read("a", entry.ID)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "segment.mp4")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(f, r)
		f.Close()
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("undecodable fragment: %v %s", err, decoded)
		}
	}
}
