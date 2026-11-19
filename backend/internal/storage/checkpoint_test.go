package storage_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"vms.local/backend/internal/storage"
)

func TestRecordingSnapshotPublishesOnlyCommittedPrefix(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetLimit("tenant", storage.Recording, 1<<20); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateCapture("tenant", 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Snapshot("tenant", w.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("uncommitted file readable", err)
	}
	w.Write([]byte("first"))
	if err = w.Checkpoint(json.RawMessage(`{"parts":1}`)); err != nil {
		t.Fatal(err)
	}
	first := s.Catalog(0, 100)
	if len(first) != 1 || first[0].Finalized || first[0].Size != 5 {
		t.Fatalf("active checkpoint not published: %+v", first)
	}
	r, entry, err := s.Snapshot("tenant", w.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w.Write([]byte("second"))
	if err = w.Grow(16384, true); err != nil {
		t.Fatal(err)
	}
	beforeCheckpoint := s.Catalog(0, 100)
	if len(beforeCheckpoint) != 1 || beforeCheckpoint[0].Size != 5 || string(beforeCheckpoint[0].Metadata) != `{"parts":1}` {
		t.Fatalf("Grow published uncommitted tail: %+v", beforeCheckpoint)
	}
	if err = w.Checkpoint(json.RawMessage(`{"parts":2}`)); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("uncommitted"))
	raw, err := io.ReadAll(r)
	if err != nil || string(raw) != "first" || string(entry.Metadata) != `{"parts":1}` {
		t.Fatalf("snapshot changed: %q %+v %v", raw, entry, err)
	}
	if end, err := r.Seek(0, io.SeekEnd); err != nil || end != 5 {
		t.Fatal("seek exposed tail", end, err)
	}
	next := s.Catalog(first[0].Sequence, 100)
	if len(next) != 1 || next[0].ID != w.ID() || next[0].Size != 11 || next[0].Sequence <= first[0].Sequence {
		t.Fatalf("checkpoint revision missing: %+v", next)
	}
	r2, e2, err := s.Snapshot("tenant", w.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	raw, err = io.ReadAll(r2)
	if err != nil || string(raw) != "firstsecond" || e2.Size != 11 {
		t.Fatalf("uncommitted tail exposed: %q %v", raw, err)
	}
	if _, _, err = s.Snapshot("other-tenant", w.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("tenant isolation", err)
	}
	if _, err = s.Read("tenant", w.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("legacy immutable reader accepted active file", err)
	}
	if err = w.FinishWithMetadata(json.RawMessage(`{"parts":3}`)); err != nil {
		t.Fatal(err)
	}
	raw = nil
	r.Seek(0, io.SeekStart)
	raw, err = io.ReadAll(r)
	if err != nil || string(raw) != "first" {
		t.Fatalf("finalization changed open snapshot: %q %v", raw, err)
	}
}

func TestCrashRecoversOnlyDurableRecordingCheckpoint(t *testing.T) {
	if root := os.Getenv("VMS_CHECKPOINT_CHILD"); root != "" {
		s, e := storage.Open(root, storage.Options{VolumeLimit: 1 << 20})
		if e != nil {
			os.Exit(10)
		}
		if s.SetLimit("tenant", storage.Recording, 1<<20) != nil {
			os.Exit(11)
		}
		w, e := s.Create("tenant", storage.Recording, 8192)
		if e != nil {
			os.Exit(12)
		}
		if _, e = w.Write([]byte("complete fragment")); e != nil {
			os.Exit(13)
		}
		if w.Checkpoint(json.RawMessage(`{"end":"durable"}`)) != nil {
			os.Exit(14)
		}
		w.Write([]byte("uncommitted tail"))
		os.Exit(0)
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecoversOnlyDurableRecordingCheckpoint$")
	cmd.Env = append(os.Environ(), "VMS_CHECKPOINT_CHILD="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture: %v %s", err, out)
	}
	s, err := storage.Open(root, storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := s.Entries("tenant", storage.Recording)
	if len(entries) != 1 || !entries[0].Finalized || entries[0].Size != 17 || entries[0].Charged != 4096 {
		t.Fatalf("recovery: %+v", entries)
	}
	r, err := s.Read("tenant", entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	raw, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(raw, []byte("complete fragment")) {
		t.Fatalf("incomplete tail exposed: %q %v", raw, err)
	}
	if string(entries[0].Metadata) != `{"end":"durable"}` {
		t.Fatal("checkpoint metadata lost")
	}
}

func TestCorruptedCheckpointIsUnavailableAndRemainsCharged(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecoversOnlyDurableRecordingCheckpoint$")
	cmd.Env = append(os.Environ(), "VMS_CHECKPOINT_CHILD="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkpoint fixture %v %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.media"))
	if err != nil || len(files) != 1 {
		t.Fatal("missing checkpoint file")
	}
	f, err := os.OpenFile(files[0], os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte("X"), 0)
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(root, storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := s.Entries("tenant", storage.Recording)
	catalog := s.Catalog(0, 10)
	if len(entries) != 1 || entries[0].Finalized || s.Usage("tenant", storage.Recording) == 0 || len(catalog) != 1 || !catalog[0].Deleted {
		t.Fatal("corrupt checkpoint published or released its charge")
	}
	if _, err = s.Read("tenant", entries[0].ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("corrupt checkpoint readable")
	}
}
