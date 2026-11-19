package media

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"vms.local/backend/internal/storage"
)

func TestArchiveHTTPOnlyServesDurablePrefix(t *testing.T) {
	store, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SetLimit("tenant", storage.Recording, 1<<20); err != nil {
		t.Fatal(err)
	}
	writer, err := store.CreateCapture("tenant", 4096)
	if err != nil {
		t.Fatal(err)
	}
	writer.Write([]byte("committed"))
	if err = writer.Checkpoint(json.RawMessage(`{"parts":1}`)); err != nil {
		t.Fatal(err)
	}
	writer.Write([]byte("uncommitted-tail"))
	service := &ArchiveService{store: store}
	for _, scenario := range []struct {
		header string
		status int
		body   string
	}{{"", 200, "committed"}, {"bytes=2-4", 206, "mmi"}, {"bytes=9-", 416, "invalid range: failed to overlap\n"}} {
		req := httptest.NewRequest("GET", "/archive/objects/"+writer.ID()+"?tenant=tenant", nil)
		if scenario.header != "" {
			req.Header.Set("Range", scenario.header)
		}
		result := httptest.NewRecorder()
		service.ServeHTTP(result, req)
		if result.Code != scenario.status || result.Body.String() != scenario.body {
			t.Fatalf("range %q: %d %q", scenario.header, result.Code, result.Body.String())
		}
	}
}

func TestExportSnapshotPreservesPreviewAcrossAppends(t *testing.T) {
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	first := PartIndex{ArchivePart: ArchivePart{Start: at, End: at.Add(time.Second)}, Offset: 20, Length: 100}
	original := SegmentIndex{Tenant: "tenant", Camera: "camera", Site: "site", Source: "source", Profile: "main", Start: at, End: first.End, InitLength: 20, Parts: []PartIndex{first}}
	clip := ExportClip{ID: "object", MetadataHash: IndexHash(original), Index: original}
	clip.Index.Parts = nil
	current := original
	current.Parts = append(append([]PartIndex{}, current.Parts...), PartIndex{ArchivePart: ArchivePart{Start: first.End, End: at.Add(2 * time.Second)}, Offset: 120, Length: 100})
	current.End = at.Add(2 * time.Second)
	snapshot, ok := ExportSnapshot(current, clip)
	if !ok || len(snapshot.Parts) != 1 || !snapshot.End.Equal(first.End) {
		t.Fatal("append invalidated preview or extended export", snapshot)
	}
	current.Parts[0].Length++
	if _, ok = ExportSnapshot(current, clip); ok {
		t.Fatal("changed selected prefix was accepted")
	}
}
