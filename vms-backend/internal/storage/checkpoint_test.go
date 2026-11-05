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
	if len(entries) != 1 || entries[0].Finalized || s.Usage("tenant", storage.Recording) == 0 || len(s.Catalog(0, 10)) != 0 {
		t.Fatal("corrupt checkpoint published or released its charge")
	}
	if _, err = s.Read("tenant", entries[0].ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("corrupt checkpoint readable")
	}
}
