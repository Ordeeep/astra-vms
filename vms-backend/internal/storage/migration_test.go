package storage_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"
	"vms.local/backend/internal/storage"
)

func TestLegacyArchiveMigratesQuotaCatalogAndIdempotencyTogether(t *testing.T) {
	root := t.TempDir()
	one := storage.Entry{ID: "legacy-one", Tenant: "tenant", Kind: storage.Recording, Created: time.Unix(1000, 0), Charged: 4096, Size: 4096, Finalized: true, Sequence: 7, Metadata: json.RawMessage(`{"origin":"legacy"}`)}
	two := one
	two.ID = "legacy-two"
	two.Sequence = 8
	two.Created = time.Unix(2000, 0)
	for _, id := range []string{one.ID, two.ID} {
		if err := os.WriteFile(filepath.Join(root, id+".media"), bytes.Repeat([]byte("v"), 4096), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"Sequence": 7, "Entries": map[string]any{one.ID: one}, "Limits": map[string]int64{"tenant/recording": 8192}, "Operations": map[string]any{"tenant/recording/original-command": map[string]any{"ID": one.ID, "Capacity": 4096}}}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(root, "index.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"Entry": two})
	journal := make([]byte, 8)
	binary.BigEndian.PutUint32(journal[:4], uint32(len(raw)))
	binary.BigEndian.PutUint32(journal[4:], crc32.ChecksumIEEE(raw))
	journal = append(journal, raw...)
	journal = append(journal, 0, 0, 0)
	if err := os.WriteFile(filepath.Join(root, "ledger.log"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	opts := storage.Options{VolumeLimit: 16384}
	s, err := storage.Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Usage("tenant", storage.Recording) != 8192 {
		t.Fatal("migration changed allocation accounting")
	}
	if _, err = s.Create("tenant", storage.Recording, 4096); !errors.Is(err, storage.ErrCapacity) {
		t.Fatal("migration reset hard quota")
	}
	page := s.Catalog(7, 10)
	if len(page) != 1 || page[0].ID != two.ID || string(page[0].Metadata) != string(one.Metadata) {
		t.Fatal("journal catalogue not preserved")
	}
	replay, err := s.CreateOperation("tenant", storage.Recording, 4096, "original-command")
	if err != nil || replay.ID != one.ID || !replay.Finalized {
		t.Fatal("idempotency identity not preserved")
	}
	if err = s.ClaimCapturePlan("retired-generation"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ClaimCapturePlan("retired-generation"); err == nil {
		t.Fatal("retired generation reopened after restart")
	}
	if s.Usage("tenant", storage.Recording) != 8192 || len(s.Catalog(0, 10)) != 2 {
		t.Fatal("new index restart lost migrated state")
	}
	if _, err = os.Stat(filepath.Join(root, "index.json.legacy")); err != nil {
		t.Fatal("migration source was not preserved")
	}
}
