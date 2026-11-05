package media

import (
	"encoding/json"
	"time"
	"vms.local/backend/internal/storage"
)

func (a *ArchiveService) cleanExportArtifacts(startup bool) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return
	}
	cursor := ""
	for {
		items, next, err := store.ObjectsPage("", storage.Export, cursor, 100)
		if err != nil {
			return
		}
		for _, entry := range items {
			if !entry.Finalized {
				if startup || !entry.Writing {
					store.Delete(entry.Tenant, entry.ID, false)
				}
				continue
			}
			var meta exportArtifact
			if json.Unmarshal(entry.Metadata, &meta) == nil && !meta.Result.Expires.IsZero() && !time.Now().Before(meta.Result.Expires) {
				store.Delete(entry.Tenant, entry.ID, false)
			}
		}
		if next == "" {
			return
		}
		cursor = next
	}
}
