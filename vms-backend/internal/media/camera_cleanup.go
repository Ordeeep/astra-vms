package media

import (
	"encoding/json"
	"errors"
	"net/http"

	"vms.local/backend/internal/storage"
)

type CameraCleanupRequest struct {
	Tenant string `json:"tenant"`
	Camera string `json:"camera"`
	Cursor string `json:"cursor"`
}
type CameraCleanupPage struct {
	NextCursor string `json:"nextCursor"`
	Done       bool   `json:"done"`
}

// Scan the worker catalogue rather than the core snapshot: a final recording
// may have been sealed after the last SQL import. Pages keep memory bounded.
func (a *ArchiveService) cameraCleanupHTTP(w http.ResponseWriter, r *http.Request, store *storage.Store) {
	var in CameraCleanupRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil || in.Tenant == "" || in.Camera == "" {
		http.Error(w, "tenant and camera required", 422)
		return
	}
	// Never delete while a capture can still finish another object.
	a.mu.Lock()
	active := false
	for _, c := range a.captures {
		if c.plan.Tenant == in.Tenant && c.plan.Camera == in.Camera {
			active = true
			break
		}
	}
	a.mu.Unlock()
	if active {
		http.Error(w, "capture still active", 409)
		return
	}
	entries, next, err := store.ObjectsPage(in.Tenant, storage.Recording, in.Cursor, 100)
	if err != nil {
		http.Error(w, "catalogue unavailable", 503)
		return
	}
	for _, entry := range entries {
		if r.Context().Err() != nil {
			return
		}
		var index SegmentIndex
		if json.Unmarshal(entry.Metadata, &index) != nil || index.Tenant != in.Tenant || index.Camera == "" {
			http.Error(w, "invalid recording metadata", 503)
			return
		}
		if index.Tenant != in.Tenant || index.Camera != in.Camera {
			continue
		}
		if err = store.Delete(in.Tenant, entry.ID, false); err != nil && !errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "recording protected or unavailable", 409)
			return
		}
	}
	archiveJSON(w, 200, CameraCleanupPage{NextCursor: next, Done: next == ""})
}
