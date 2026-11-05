package storage_test

import (
	"testing"
	"vms.local/backend/internal/storage"
)

func TestOverwriteTombstoneSurvivesRestartUntilAcknowledged(t *testing.T) {
	root := t.TempDir()
	options := storage.Options{VolumeLimit: 4096}
	s, err := storage.Open(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetLimit("tenant", storage.Recording, 4096); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateCapture("tenant", 4096)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("old footage"))
	if err = w.Finish(); err != nil {
		t.Fatal(err)
	}
	old := w.ID()
	first := s.Catalog(0, 100)[0].Sequence
	w, err = s.CreateCapture("tenant", 4096)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("new footage"))
	if err = w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(root, options)
	if err != nil {
		t.Fatal(err)
	}
	items := s.Catalog(first, 100)
	if len(items) != 2 || !items[0].Deleted || items[0].ID != old || items[1].ID != w.ID() {
		t.Fatalf("catalogue lost ordered overwrite: %+v", items)
	}
	if err = s.Acknowledge(items[0].Sequence); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = storage.Open(root, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items = s.Catalog(0, 100)
	if len(items) != 1 || items[0].Deleted || items[0].ID != w.ID() {
		t.Fatalf("acknowledged deletion reappeared: %+v", items)
	}
}
