//go:build linux

package storage_test

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"vms.local/backend/internal/storage"
)

func measuredAllocation(t *testing.T, root string) int64 {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "*.media"))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, path := range paths {
		var info syscall.Stat_t
		if err := syscall.Stat(path, &info); err != nil {
			t.Fatal(err)
		}
		total += info.Blocks * 512
	}
	return total
}

func TestLinuxGrowingVideoKeepsExtentMetadataInsideReservation(t *testing.T) {
	root := t.TempDir()
	s, err := storage.Open(root, storage.Options{VolumeLimit: 32 << 20, Trace: func(event storage.Event) {
		if physical := measuredAllocation(t, root); physical > event.Total {
			t.Errorf("%s: physical=%d reservation=%d", event.Operation, physical, event.Total)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetLimit("video", storage.Recording, 32<<20); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateCapture("video", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	for part := int64(1); part <= 96; part++ {
		capacity := 4096 + part*8193
		if err = w.Grow(capacity, true); err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(make([]byte, 8193)); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := s.Usage("video", storage.Recording); got != measuredAllocation(t, root) {
		t.Fatalf("final charge=%d differs from physical allocation", got)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(root, storage.Options{VolumeLimit: 32 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entry, err := s.Entry("video", w.ID())
	if err != nil || !entry.Finalized || entry.Size != 4096+96*8193 {
		t.Fatalf("growing recording did not survive reopen: %+v %v", entry, err)
	}
}

// A nonempty directory at the object path causes a real unlink failure,
// including when the suite runs as root. The allocated file remains on disk.
func preventDelete(t *testing.T, path string) func() {
	t.Helper()
	held := path + ".held"
	if err := os.Rename(path, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "block"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := os.Remove(filepath.Join(path, "block")); err != nil {
				t.Error(err)
			}
			if err := os.Remove(path); err != nil {
				t.Error(err)
			}
			if err := os.Rename(held, path); err != nil {
				t.Error(err)
			}
		})
	}
}
