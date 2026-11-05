package media

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vms.local/backend/internal/storage"
)

func TestExportExpiryReleasesOnlyExpiredUnprotectedArtifacts(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(root, storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, kind := range []storage.Kind{storage.Recording, storage.Export} {
		if err := store.SetLimit("tenant", kind, 1<<19); err != nil {
			t.Fatal(err)
		}
	}
	write := func(kind storage.Kind, expires time.Time) string {
		t.Helper()
		writer, err := store.Create("tenant", kind, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("artifact")); err != nil {
			t.Fatal(err)
		}
		metadata, _ := json.Marshal(exportArtifact{Result: ExportStatus{Expires: expires}})
		if err := writer.FinishWithMetadata(metadata); err != nil {
			t.Fatal(err)
		}
		return writer.ID()
	}
	expired := write(storage.Export, time.Now().Add(-time.Hour))
	unexpired := write(storage.Export, time.Now().Add(time.Hour))
	recording := write(storage.Recording, time.Now().Add(-time.Hour))
	reader, err := store.Read("tenant", expired)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	service := &ArchiveService{store: store}
	before := store.Usage("tenant", storage.Export)
	service.cleanExportArtifacts(false)
	if store.Usage("tenant", storage.Export) != before {
		t.Fatal("cleanup released capacity while the expired artifact was protected")
	}
	if _, err := os.Stat(filepath.Join(root, expired+".media")); err != nil {
		t.Fatal("protected artifact disappeared", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	service.cleanExportArtifacts(false)
	if _, err := os.Stat(filepath.Join(root, expired+".media")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired export was not physically deleted", err)
	}
	if store.Usage("tenant", storage.Export) >= before {
		t.Fatal("deleted artifact did not release its export budget")
	}
	for _, id := range []string{unexpired, recording} {
		if _, err := os.Stat(filepath.Join(root, id+".media")); err != nil {
			t.Fatal("cleanup removed an unexpired export or recording", err)
		}
	}
}
