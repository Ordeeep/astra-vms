package storage_test

import (
	"context"
	"testing"
	"time"
	"vms.local/backend/internal/storage"
)

func TestAdministrativeReductionPreservesProtectedHistoryAndOldLimit(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 16384})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetLimit("tenant", storage.Recording, 12288)
	for range 3 {
		w, e := s.Create("tenant", storage.Recording, 4096)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte{1})
		w.Finish()
	}
	entries := s.Entries("tenant", storage.Recording)
	protected, err := s.Read("tenant", entries[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ReduceRecording(context.Background(), "tenant", 0, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || !result.Protected || result.Usage != 4096 || result.Limit != 12288 {
		t.Fatalf("premature quota application: %+v", result)
	}
	var content [1]byte
	if _, err = protected.Read(content[:]); err != nil {
		t.Fatal("administrative reduction preempted reader")
	}
	protected.Close()
	result, err = s.ReduceRecording(context.Background(), "tenant", 0, time.Now().Add(time.Second))
	if err != nil || !result.Applied || result.Usage != 0 || result.Limit != 0 {
		t.Fatalf("quota not applied after release: %+v %v", result, err)
	}
	if _, err = s.Create("tenant", storage.Recording, 1); err == nil {
		t.Fatal("new write exceeded reduced quota")
	}
}
