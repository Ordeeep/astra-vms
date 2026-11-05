package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var indexBuckets = []string{"entries", "metadata", "chronology", "tenant_chronology", "catalog", "deleted", "limits", "operations", "plans", "usage", "control"}

func bucket(tx *bolt.Tx, name string) *bolt.Bucket { return tx.Bucket([]byte(name)) }
func number(n int64) []byte                        { var b [8]byte; binary.BigEndian.PutUint64(b[:], uint64(n)); return b[:] }
func integer(raw []byte) int64 {
	if len(raw) != 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(raw))
}
func chronology(e *Entry) []byte {
	return []byte(fmt.Sprintf("%s/%020d/%s", e.Kind, e.Created.UnixNano(), e.ID))
}
func tenantChronology(e *Entry) []byte { return append([]byte(e.Tenant+"/"), chronology(e)...) }

func (s *Store) openIndex() error {
	db, err := bolt.Open(filepath.Join(s.root, "ledger.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return err
	}
	s.index = db
	ready := false
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range indexBuckets {
			if _, e := tx.CreateBucketIfNotExists([]byte(name)); e != nil {
				return e
			}
		}
		format := bucket(tx, "control").Get([]byte("format"))
		if len(format) > 0 && !bytes.Equal(format, []byte("2")) {
			return errors.New("storage format requires another worker version")
		}
		ready = bytes.Equal(format, []byte("2"))
		return nil
	})
	if err != nil {
		return err
	}
	if !ready {
		// Adoption of the pre-release journal is one-time. It remains authoritative
		// until the transaction with the format marker commits. Interrupted adoption
		// is replayed from the preserved journal, never from a partial new index.
		legacy := Store{root: s.root, state: state{Entries: map[string]*Entry{}, Operations: map[string]reservationRecord{}, Limits: map[string]int64{}, Deleted: map[string]*Entry{}}}
		raw, e := os.ReadFile(filepath.Join(s.root, "index.json"))
		if e == nil {
			e = json.Unmarshal(raw, &legacy.state)
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if legacy.state.Entries == nil || legacy.state.Limits == nil {
			return errors.New("invalid legacy storage index")
		}
		if legacy.state.Deleted == nil {
			legacy.state.Deleted = map[string]*Entry{}
		}
		if legacy.state.Operations == nil {
			legacy.state.Operations = map[string]reservationRecord{}
		}
		if _, e = os.Stat(filepath.Join(s.root, "ledger.log")); e == nil {
			if e = legacy.openJournal(); e != nil {
				return e
			}
			if e = legacy.journal.Close(); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		err = db.Update(func(tx *bolt.Tx) error {
			// A previously interrupted adoption has no format marker.
			for _, name := range indexBuckets {
				if e := tx.DeleteBucket([]byte(name)); e != nil {
					return e
				}
				if _, e := tx.CreateBucket([]byte(name)); e != nil {
					return e
				}
			}
			for _, entry := range legacy.state.Entries {
				if e := applyIndexChange(tx, ledgerChange{Entry: entry}); e != nil {
					return e
				}
			}
			for _, entry := range legacy.state.Deleted {
				if e := applyIndexChange(tx, ledgerChange{Tombstone: entry}); e != nil {
					return e
				}
			}
			for k, n := range legacy.state.Limits {
				if e := bucket(tx, "limits").Put([]byte(k), number(n)); e != nil {
					return e
				}
			}
			for k, v := range legacy.state.Operations {
				raw, _ := json.Marshal(v)
				if e := bucket(tx, "operations").Put([]byte(k), raw); e != nil {
					return e
				}
			}
			if e := bucket(tx, "control").Put([]byte("sequence"), number(legacy.state.Sequence)); e != nil {
				return e
			}
			return bucket(tx, "control").Put([]byte("format"), []byte("2"))
		})
		if err != nil {
			return err
		}
	}
	// Preserve migration sources, while preventing a previous journal-only
	// binary from silently opening stale accounting after a downgrade.
	for _, name := range []string{"index.json", "ledger.log"} {
		path := filepath.Join(s.root, name)
		if _, e := os.Stat(path); e == nil {
			if e = os.Rename(path, path+".legacy"); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	s.state = state{Limits: map[string]int64{}}
	s.charges = map[string]int64{}
	return db.View(func(tx *bolt.Tx) error {
		s.state.Sequence = integer(bucket(tx, "control").Get([]byte("sequence")))
		if e := bucket(tx, "limits").ForEach(func(k, v []byte) error { s.state.Limits[string(k)] = integer(v); return nil }); e != nil {
			return e
		}
		return bucket(tx, "usage").ForEach(func(k, v []byte) error { s.charges[string(k)] = integer(v); s.totalCharge += integer(v); return nil })
	})
}

// A retired capture generation cannot reopen after process restart. The
// ledger keeps only its opaque ID; camera credentials never enter this index.
func (s *Store) ClaimCapturePlan(id string) error {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if !validTenant.MatchString(id) {
		return errors.New("invalid capture identity")
	}
	retired := errors.New("retired capture generation")
	err := s.index.Update(func(tx *bolt.Tx) error {
		plans := bucket(tx, "plans")
		if plans.Get([]byte(id)) != nil {
			return retired
		}
		return plans.Put([]byte(id), []byte{1})
	})
	if err != nil && !errors.Is(err, retired) {
		s.fault = err
	}
	return err
}

func applyIndexChange(tx *bolt.Tx, c ledgerChange) error {
	entries, details := bucket(tx, "entries"), bucket(tx, "metadata")
	id := c.Deleted
	if c.Entry != nil {
		id = c.Entry.ID
	}
	if id != "" {
		old := entries.Get([]byte(id))
		var previous Entry
		if old != nil {
			if err := json.Unmarshal(old, &previous); err != nil {
				return err
			}
			if previous.Sequence > 0 {
				if err := bucket(tx, "catalog").Delete(number(previous.Sequence)); err != nil {
					return err
				}
			}
			for _, item := range []struct {
				name string
				key  []byte
			}{{"chronology", chronology(&previous)}, {"tenant_chronology", tenantChronology(&previous)}} {
				if err := bucket(tx, item.name).Delete(item.key); err != nil {
					return err
				}
			}
			usageKey := []byte(key(previous.Tenant, previous.Kind))
			n := integer(bucket(tx, "usage").Get(usageKey)) - previous.Charged
			if n < 0 {
				return errors.New("invalid allocation accounting")
			}
			if err := bucket(tx, "usage").Put(usageKey, number(n)); err != nil {
				return err
			}
		}
		if c.Entry == nil {
			if err := entries.Delete([]byte(id)); err != nil {
				return err
			}
			if err := details.Delete([]byte(id)); err != nil {
				return err
			}
		} else {
			copy := *c.Entry
			copy.Metadata = nil
			raw, err := json.Marshal(copy)
			if err != nil {
				return err
			}
			if err = entries.Put([]byte(id), raw); err != nil {
				return err
			}
			if err = details.Put([]byte(id), c.Entry.Metadata); err != nil {
				return err
			}
			for _, item := range []struct {
				name string
				key  []byte
			}{{"chronology", chronology(&copy)}, {"tenant_chronology", tenantChronology(&copy)}} {
				if err = bucket(tx, item.name).Put(item.key, []byte(id)); err != nil {
					return err
				}
			}
			usageKey := []byte(key(copy.Tenant, copy.Kind))
			if err = bucket(tx, "usage").Put(usageKey, number(integer(bucket(tx, "usage").Get(usageKey))+copy.Charged)); err != nil {
				return err
			}
			if copy.Finalized && copy.Sequence > 0 {
				if err = bucket(tx, "catalog").Put(number(copy.Sequence), append([]byte("e"), []byte(id)...)); err != nil {
					return err
				}
			}
		}
	}
	if c.Tombstone != nil {
		raw, err := json.Marshal(c.Tombstone)
		if err != nil {
			return err
		}
		if err = bucket(tx, "deleted").Put(number(c.Tombstone.Sequence), raw); err != nil {
			return err
		}
		if err = bucket(tx, "catalog").Put(number(c.Tombstone.Sequence), append([]byte("d"), raw...)); err != nil {
			return err
		}
	}
	if c.Acknowledged > 0 {
		cursor := bucket(tx, "deleted").Cursor()
		for k, _ := cursor.First(); k != nil && integer(k) <= c.Acknowledged; k, _ = cursor.Next() {
			if err := bucket(tx, "catalog").Delete(k); err != nil {
				return err
			}
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
	}
	if c.LimitKey != "" {
		if err := bucket(tx, "limits").Put([]byte(c.LimitKey), number(c.Limit)); err != nil {
			return err
		}
	}
	if c.OperationKey != "" {
		raw, err := json.Marshal(c.Operation)
		if err != nil {
			return err
		}
		if err = bucket(tx, "operations").Put([]byte(c.OperationKey), raw); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) commit(c ledgerChange) error {
	var delta int64
	usageKey := ""
	err := s.index.Update(func(tx *bolt.Tx) error {
		id := c.Deleted
		if c.Entry != nil {
			id = c.Entry.ID
			usageKey = key(c.Entry.Tenant, c.Entry.Kind)
			delta = c.Entry.Charged
		}
		if id != "" {
			if raw := bucket(tx, "entries").Get([]byte(id)); raw != nil {
				var old Entry
				if e := json.Unmarshal(raw, &old); e != nil {
					return e
				}
				usageKey = key(old.Tenant, old.Kind)
				delta -= old.Charged
			}
		}
		if e := applyIndexChange(tx, c); e != nil {
			return e
		}
		return bucket(tx, "control").Put([]byte("sequence"), number(s.state.Sequence))
	})
	if err != nil {
		s.fault = err
		return err
	}
	if usageKey != "" {
		s.charges[usageKey] += delta
		s.totalCharge += delta
	}
	return nil
}

func indexEntry(tx *bolt.Tx, id string, metadata bool) (*Entry, error) {
	raw := bucket(tx, "entries").Get([]byte(id))
	if raw == nil {
		return nil, nil
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	if metadata {
		e.Metadata = bytesClone(bucket(tx, "metadata").Get([]byte(id)))
	}
	return &e, nil
}
func (s *Store) getEntry(id string, metadata bool) *Entry {
	if w := s.writers[id]; w != nil {
		copy := *w.entry
		if metadata {
			copy.Metadata = bytesClone(copy.Metadata)
		} else {
			copy.Metadata = nil
		}
		return &copy
	}
	var entry *Entry
	if err := s.index.View(func(tx *bolt.Tx) error { var e error; entry, e = indexEntry(tx, id, metadata); return e }); err != nil {
		s.fault = err
	}
	return entry
}
func (s *Store) objectPage(tenant string, kind Kind, after string, limit int, metadata bool) []Entry {
	items := []Entry{}
	err := s.index.View(func(tx *bolt.Tx) error {
		name, prefix := "chronology", string(kind)+"/"
		if tenant != "" {
			name = "tenant_chronology"
			prefix = tenant + "/" + prefix
		}
		cursor := bucket(tx, name).Cursor()
		seek := []byte(prefix)
		if after != "" {
			seek = []byte(after)
		}
		for k, v := cursor.Seek(seek); k != nil && bytes.HasPrefix(k, []byte(prefix)); k, v = cursor.Next() {
			if after != "" && string(k) <= after {
				continue
			}
			e, err := indexEntry(tx, string(v), metadata)
			if err != nil {
				return err
			}
			if e == nil {
				return errors.New("missing indexed object")
			}
			e.Writing = s.writers[e.ID] != nil
			items = append(items, *e)
			if len(items) >= limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		s.fault = err
		return nil
	}
	return items
}
func (s *Store) oldestRecording(tenant string, allowProtected bool) *Entry {
	var unprotected, protected *Entry
	err := s.index.View(func(tx *bolt.Tx) error {
		name, prefix := "chronology", "recording/"
		if tenant != "" {
			name = "tenant_chronology"
			prefix = tenant + "/" + prefix
		}
		cursor := bucket(tx, name).Cursor()
		for k, id := cursor.Seek([]byte(prefix)); k != nil && bytes.HasPrefix(k, []byte(prefix)); k, id = cursor.Next() {
			if s.writers[string(id)] != nil {
				continue
			}
			if len(s.readers[string(id)]) > 0 {
				if allowProtected && protected == nil {
					var e error
					protected, e = indexEntry(tx, string(id), false)
					if e != nil {
						return e
					}
				}
				continue
			}
			var e error
			unprotected, e = indexEntry(tx, string(id), false)
			return e
		}
		return nil
	})
	if err != nil {
		s.fault = err
		return nil
	}
	if unprotected != nil {
		return unprotected
	}
	return protected
}

func (s *Store) catalogPage(after int64, limit int) []Entry {
	entries := []Entry{}
	if limit < 1 || limit > 1000 {
		return entries
	}
	err := s.index.View(func(tx *bolt.Tx) error {
		c := bucket(tx, "catalog").Cursor()
		for k, v := c.Seek(number(after + 1)); k != nil; k, v = c.Next() {
			if len(v) < 2 {
				return errors.New("invalid catalogue index")
			}
			var e *Entry
			if v[0] == 'e' {
				var err error
				e, err = indexEntry(tx, string(v[1:]), true)
				if err != nil {
					return err
				}
			} else {
				e = &Entry{}
				if err := json.Unmarshal(v[1:], e); err != nil {
					return err
				}
			}
			if e == nil {
				return errors.New("missing catalogue object")
			}
			entries = append(entries, *e)
			if len(entries) == limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		s.fault = err
		return nil
	}
	return entries
}

// ObjectsPage returns at most limit objects, and an opaque cursor. The worker
// uses this for cleanup without buffering all artifacts on the volume.
func (s *Store) ObjectsPage(tenant string, kind Kind, after string, limit int) ([]Entry, string, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		return nil, "", errors.New("invalid object page size")
	}
	items := s.objectPage(tenant, kind, after, limit, true)
	if s.fault != nil {
		return nil, "", ErrUnavailable
	}
	next := ""
	if len(items) == limit {
		last := &items[len(items)-1]
		if tenant == "" {
			next = string(chronology(last))
		} else {
			next = string(tenantChronology(last))
		}
	}
	return items, next, nil
}
