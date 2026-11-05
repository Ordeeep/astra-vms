package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"vms.local/backend/internal/storage"
)

func cleanupObject(t *testing.T, s *storage.Store, tenant, camera string, kind storage.Kind) string {
	t.Helper()
	w, err := s.Create(tenant, kind, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("video fixture")); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(SegmentIndex{Tenant: tenant, Camera: camera})
	if err = w.FinishWithMetadata(raw); err != nil {
		t.Fatal(err)
	}
	return w.ID()
}
func cleanupPage(t *testing.T, a *ArchiveService, s *storage.Store, in CameraCleanupRequest, want int) CameraCleanupPage {
	t.Helper()
	raw, _ := json.Marshal(in)
	r := httptest.NewRequest("POST", "/archive/camera-cleanup", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	a.cameraCleanupHTTP(w, r, s)
	if w.Code != want {
		t.Fatalf("cleanup status %d, want %d: %s", w.Code, want, w.Body.String())
	}
	var page CameraCleanupPage
	if want == 200 && json.Unmarshal(w.Body.Bytes(), &page) != nil {
		t.Fatal("invalid cleanup page")
	}
	return page
}
func TestCameraCleanupPagesProtectReadersAndPreserveOtherCamerasTenantsAndExports(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tenant := range []string{"a", "b"} {
		if err = s.SetLimit(tenant, storage.Recording, 4<<20); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SetLimit("a", storage.Export, 1<<20); err != nil {
		t.Fatal(err)
	}
	a := &ArchiveService{captures: map[string]*capture{}}
	targets := []string{}
	for i := 0; i < 125; i++ {
		targets = append(targets, cleanupObject(t, s, "a", "camera", storage.Recording))
	}
	preserved := []struct{ tenant, id string }{
		{"a", cleanupObject(t, s, "a", "other", storage.Recording)},
		{"b", cleanupObject(t, s, "b", "camera", storage.Recording)},
		{"a", cleanupObject(t, s, "a", "camera", storage.Export)},
	}
	in := CameraCleanupRequest{Tenant: "a", Camera: "camera"}
	reader, err := s.Read("a", targets[0])
	if err != nil {
		t.Fatal(err)
	}
	cleanupPage(t, a, s, in, 409)
	if _, err = s.Entry("a", targets[0]); err != nil {
		t.Fatal("protected file removed")
	}
	reader.Close()
	pages := 0
	for {
		page := cleanupPage(t, a, s, in, 200)
		pages++
		if page.Done {
			break
		}
		if page.NextCursor == "" || pages > 5 {
			t.Fatal("invalid page progress")
		}
		in.Cursor = page.NextCursor
	}
	if pages < 2 {
		t.Fatal("cleanup did not paginate")
	}
	for _, id := range targets {
		if _, err = s.Entry("a", id); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("target remained: %v", err)
		}
	}
	for _, entry := range preserved {
		if _, err = s.Entry(entry.tenant, entry.id); err != nil {
			t.Fatal("unrelated object removed", err)
		}
	}
	cleanupPage(t, a, s, CameraCleanupRequest{Camera: "camera"}, 422)
	cleanupPage(t, a, s, CameraCleanupRequest{Tenant: "a", Camera: "camera"}, 200)
}

func TestCameraStopWaitsForLastSegmentWithoutStoppingOtherCamera(t *testing.T) {
	s, err := storage.Open(t.TempDir(), storage.Options{VolumeLimit: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetLimit("a", storage.Recording, 1<<20); err != nil {
		t.Fatal(err)
	}
	writer, err := s.Create("a", storage.Recording, 4096)
	if err != nil {
		t.Fatal(err)
	}
	writer.Write([]byte("last segment"))
	stopped, cancel := context.WithCancel(context.Background())
	defer cancel()
	other, otherCancel := context.WithCancel(context.Background())
	defer otherCancel()
	c := &capture{plan: CapturePlan{Tenant: "a", Camera: "camera"}, cancel: cancel, done: make(chan struct{})}
	a := &ArchiveService{store: s, captures: map[string]*capture{"last": c, "other": {plan: CapturePlan{Tenant: "a", Camera: "other"}, cancel: otherCancel, done: make(chan struct{})}}}
	in := CameraCleanupRequest{Tenant: "a", Camera: "camera"}
	cleanupPage(t, a, s, in, 409)
	finished := make(chan error, 1)
	go func() {
		<-stopped.Done()
		raw, _ := json.Marshal(SegmentIndex{Tenant: "a", Camera: "camera"})
		e := writer.FinishWithMetadata(raw)
		a.mu.Lock()
		delete(a.captures, "last")
		a.mu.Unlock()
		close(c.done)
		finished <- e
	}()
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("DELETE", "/captures?tenant=a&camera=camera", nil))
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	if other.Err() != nil {
		t.Fatal("another camera was stopped")
	}
	if !cleanupPage(t, a, s, in, 200).Done {
		t.Fatal("final segment cleanup incomplete")
	}
	if _, err = s.Entry("a", writer.ID()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("last sealed recording remained", err)
	}
}

func TestCameraWithoutConfiguredArchiveCanCompleteCleanup(t *testing.T) {
	a := &ArchiveService{captures: map[string]*capture{}}
	raw, _ := json.Marshal(CameraCleanupRequest{Tenant: "a", Camera: "camera"})
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("POST", "/archive/camera-cleanup", bytes.NewReader(raw)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var page CameraCleanupPage
	json.Unmarshal(w.Body.Bytes(), &page)
	if !page.Done {
		t.Fatal("empty unconfigured archive remained pending")
	}
}
