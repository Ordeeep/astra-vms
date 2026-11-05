package storage_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"vms.local/backend/internal/storage"
)

// Independent filesystem observation is explicitly part of B04's test seam.
func measuredAllocation(t *testing.T, root string) int64 {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "*.media"))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw [24]byte
		ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx").Call(f.Fd(), 1, uintptr(unsafe.Pointer(&raw[0])), 24)
		f.Close()
		if ok == 0 {
			t.Fatal(err)
		}
		total += *(*int64)(unsafe.Pointer(&raw[0]))
	}
	return total
}

func TestPhysicalAllocationNeverExceedsReservationDuringVariableWrites(t *testing.T) {
	root := t.TempDir()
	observations := 0
	s, err := storage.Open(root, storage.Options{VolumeLimit: 16384, Trace: func(e storage.Event) {
		observations++
		actual := measuredAllocation(t, root)
		if actual > e.Total || e.Total > 16384 {
			t.Errorf("%s: actual=%d charged=%d", e.Operation, actual, e.Total)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLimit("a", storage.Recording, 16384); err != nil {
		t.Fatal(err)
	}
	w, err := s.Create("a", storage.Recording, 13000)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 4097, 200, 6000, 2702} {
		if _, err := w.Write(make([]byte, n)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := w.Write([]byte{1}); n != 0 || !errors.Is(err, storage.ErrCapacity) {
		t.Fatalf("oversize write n=%d err=%v", n, err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if observations < 13 {
		t.Fatalf("too few observations %d", observations)
	}
}

func TestSeparateExportBudgetAndFailedDeleteKeepRealCharges(t *testing.T) {
	root := t.TempDir()
	s, err := storage.Open(root, storage.Options{VolumeLimit: 8192})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []storage.Kind{storage.Recording, storage.Export} {
		if err := s.SetLimit("a", kind, 4096); err != nil {
			t.Fatal(err)
		}
	}
	w, err := s.Create("a", storage.Recording, 4096)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("video"))
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	x, err := s.Create("a", storage.Export, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("a", storage.Export, 4096); !errors.Is(err, storage.ErrCapacity) {
		t.Fatal("export must be denied")
	}
	if len(s.Entries("a", storage.Recording)) != 1 {
		t.Fatal("export evicted history")
	}
	// A foreign Windows reader without FILE_SHARE_DELETE makes deletion fail.
	paths, _ := filepath.Glob(filepath.Join(root, "*.media"))
	var locked *os.File
	for _, path := range paths {
		if filepath.Base(path) == w.ID()+".media" {
			locked, err = os.Open(path)
		}
	}
	if err != nil || locked == nil {
		t.Fatal(err)
	}
	if err := s.Delete("a", w.ID(), true); err == nil {
		t.Fatal("expected Windows sharing violation")
	}
	if s.Usage("a", storage.Recording) != 4096 {
		t.Fatal("failed delete released capacity")
	}
	locked.Close()
	if err := s.Delete("a", w.ID(), true); err != nil {
		t.Fatal(err)
	}
	x.Write([]byte("export"))
	if err := x.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestPartialRecoveryAuthorityAndExclusiveOwner(t *testing.T) {
	root := t.TempDir()
	opts := storage.Options{VolumeLimit: 8192}
	s, err := storage.Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("a", storage.Recording, 8192); err != nil {
		t.Fatal(err)
	}
	if second, err := storage.Open(root, opts); err == nil {
		second.Close()
		t.Fatal("second owner acquired live store")
	}
	w, err := s.Create("a", storage.Recording, 8192)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("partial"))
	s.SetAuthority(false)
	if _, err := s.Create("a", storage.Recording, 4096); !errors.Is(err, storage.ErrUnavailable) {
		t.Fatal("new reservation without authority")
	}
	if _, err := w.Write([]byte("within existing reservation")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("a", storage.Recording, 4096); !errors.Is(err, storage.ErrCapacity) {
		t.Fatal("reduced below usage")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Read("a", w.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("partial is advertised")
	}
	if s.Usage("a", storage.Recording) == 0 {
		t.Fatal("partial not charged")
	}
	if _, err := w.Write([]byte("old generation")); !errors.Is(err, storage.ErrUnavailable) {
		t.Fatal("old writer accepted bytes")
	}
}

func TestCaptureCanPreemptProtectedOldRecordingWithoutDeletingOtherTenant(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tenant := range []string{"a", "b"} {
		if err := s.SetLimit(tenant, storage.Recording, 4096); err != nil {
			t.Fatal(err)
		}
	}
	old, err := s.Create("a", storage.Recording, 4096)
	if err != nil {
		t.Fatal(err)
	}
	old.Write([]byte("old"))
	if err := old.Finish(); err != nil {
		t.Fatal(err)
	}
	other, err := s.Create("b", storage.Recording, 4096)
	if err != nil {
		t.Fatal(err)
	}
	other.Write([]byte("other"))
	if err := other.Finish(); err != nil {
		t.Fatal(err)
	}
	reader, err := s.Read("a", old.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("a", old.ID(), false); !errors.Is(err, storage.ErrProtected) {
		t.Fatalf("delete protected: %v", err)
	}
	next, err := s.CreateCapture("a", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(reader); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("capture must interrupt reader: %v", err)
	}
	if len(s.Entries("b", storage.Recording)) != 1 {
		t.Fatal("other tenant's history was removed")
	}
	next.Write([]byte("new"))
	if err := next.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWritersCannotSpendSameCapacity(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLimit("a", storage.Recording, 8192); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := s.Create("a", storage.Recording, 4096)
			if err == nil {
				_, err = w.Write(make([]byte, 4096))
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, storage.ErrCapacity) {
			t.Fatal(err)
		}
	}
	if success != 2 || s.Usage("a", storage.Recording) != 8192 {
		t.Fatalf("success=%d usage=%d", success, s.Usage("a", storage.Recording))
	}
}

func TestOpenRecordingIsChargedBeforeItCanAcceptBytes(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetLimit("tenant-a", storage.Recording, 8192); err != nil {
		t.Fatal(err)
	}
	w, err := s.Create("tenant-a", storage.Recording, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("video")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("tenant-a", storage.Recording, 4096); !errors.Is(err, storage.ErrCapacity) {
		t.Fatalf("open file must consume quota, got %v", err)
	}
	if got := s.Usage("tenant-a", storage.Recording); got != 8192 {
		t.Fatalf("charged=%d want=8192", got)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := s.Usage("tenant-a", storage.Recording); got != 4096 {
		t.Fatalf("charged after close=%d want=4096", got)
	}
}

func TestCaptureKeepsProtectedOldestWhenUnprotectedRecordingCanBeRemoved(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 8192})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetLimit("a", storage.Recording, 8192)
	old, _ := s.Create("a", storage.Recording, 4096)
	old.Write([]byte("protected"))
	old.Finish()
	reader, err := s.Read("a", old.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	newer, _ := s.Create("a", storage.Recording, 4096)
	newer.Write([]byte("eligible"))
	newer.Finish()
	if _, err := s.CreateCapture("a", 4096); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "protected" {
		t.Fatalf("protected reader interrupted: %s %v", got, err)
	}
	if _, err := s.Read("a", newer.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("eligible recording survived instead")
	}
}

func TestRepeatingReservationDoesNotCreateAnotherFile(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 16384})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetLimit("a", storage.Recording, 16384)
	first, err := s.CreateOperation("a", storage.Recording, 4096, "command-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateOperation("a", storage.Recording, 4096, "command-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || s.Usage("a", storage.Recording) != 4096 {
		t.Fatal("repeated reservation consumed twice")
	}
	first.Writer.Write([]byte("video"))
	first.Writer.Finish()
	replay, err := s.CreateOperation("a", storage.Recording, 4096, "command-1")
	if err != nil || replay.Writer != nil || !replay.Finalized {
		t.Fatalf("completed reservation replay: %#v %v", replay, err)
	}
	if _, err := s.CreateOperation("a", storage.Recording, 8192, "command-1"); err == nil {
		t.Fatal("different content reused operation identity")
	}
}
