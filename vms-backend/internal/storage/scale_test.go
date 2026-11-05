package storage_test

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
	"vms.local/backend/internal/storage"
)

func TestArchiveIndexHeapDoesNotRetainEveryRecordingFrameIndex(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetLimit("scale", storage.Recording, 8<<20); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(map[string]string{"frameIndex": string(bytes.Repeat([]byte("x"), 64<<10))})
	for i := 0; i < 1000; i++ {
		w, e := s.CreateCapture("scale", 4096)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte("frame")); e != nil {
			t.Fatal(e)
		}
		if e = w.FinishWithMetadata(metadata); e != nil {
			t.Fatal(e)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("Retained heap delta: %d KiB for 1000 indexes of 64 KiB", retained>>10)
	if retained > 24<<20 {
		t.Fatalf("1000 frame indexes retained %d MiB on heap; archive-sized buffers are forbidden", retained>>20)
	}
	items := s.Catalog(990, 10)
	if len(items) != 10 || len(items[0].Metadata) != len(metadata) {
		t.Fatal("bounded page lost detailed indexes")
	}
	if s.Usage("scale", storage.Recording) != 1000*4096 {
		t.Fatal("indexed accounting differs from physical admission")
	}
	runtime.KeepAlive(s)
}
