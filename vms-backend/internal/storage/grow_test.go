package storage_test

import (
	"errors"
	"testing"

	"vms.local/backend/internal/storage"
)

func TestGrowingCaptureReservesBeforeWriteAndCannotExceedQuota(t *testing.T) {
	s, e := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	unit := s.AllocationUnit()
	s.SetLimit("a", storage.Recording, 3*unit)
	first, e := s.CreateCapture("a", unit)
	if e != nil {
		t.Fatal(e)
	}
	first.Write(make([]byte, unit))
	first.Finish()
	next, e := s.CreateCapture("a", unit)
	if e != nil {
		t.Fatal(e)
	}
	next.Write(make([]byte, unit))
	if _, e = next.Write([]byte{1}); !errors.Is(e, storage.ErrCapacity) {
		t.Fatal("unreserved extension accepted")
	}
	if e = next.Grow(3*unit, true); e != nil {
		t.Fatal(e)
	}
	if s.Usage("a", storage.Recording) != 3*unit {
		t.Fatal("growth failed to reclaim and charge")
	}
	if _, e = next.Write(make([]byte, 2*unit)); e != nil {
		t.Fatal(e)
	}
	if e = next.Grow(4*unit, true); !errors.Is(e, storage.ErrCapacity) {
		t.Fatal("oversize growth accepted")
	}
	s.SetAuthority(false)
	if e = next.Grow(3*unit+1, true); !errors.Is(e, storage.ErrUnavailable) {
		t.Fatal("new reservation without authority")
	}
	next.Finish()
}
