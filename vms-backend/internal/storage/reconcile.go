package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// Scan bounded pages of small allocation headers. Detailed frame indexes stay
// on disk unless a checkpoint must be recovered or an object must be changed.
func (s *Store) reconcileIndex() error {
	after := ""
	for {
		page := []Entry{}
		err := s.index.View(func(tx *bolt.Tx) error {
			c := bucket(tx, "entries").Cursor()
			for k, _ := c.Seek([]byte(after)); k != nil; k, _ = c.Next() {
				if string(k) <= after {
					continue
				}
				e, err := indexEntry(tx, string(k), false)
				if err != nil {
					return err
				}
				if e == nil {
					return errors.New("invalid allocation index")
				}
				page = append(page, *e)
				if len(page) == 100 {
					break
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			after = e.ID
			if !validTenant.MatchString(e.ID) || !validTenant.MatchString(e.Tenant) {
				return errors.New("invalid object identity")
			}
			if err = s.reconcileObject(e); err != nil {
				return err
			}
		}
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		files, e := directory.ReadDir(100)
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".media") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".media")
			if s.getEntry(id, false) == nil {
				return errors.New("unattributed media file; reconcile before writing")
			}
		}
		if errors.Is(e, io.EOF) {
			break
		}
	}
	return s.fault
}
func (s *Store) reconcileObject(header Entry) error {
	f, err := os.OpenFile(s.path(header.ID), os.O_RDWR, 0600)
	if errors.Is(err, os.ErrNotExist) {
		s.state.Sequence++
		return s.commit(ledgerChange{Deleted: header.ID, Tombstone: &Entry{ID: header.ID, Tenant: header.Tenant, Kind: header.Kind, Sequence: s.state.Sequence, Deleted: true}})
	}
	if err != nil {
		return err
	}
	defer f.Close()
	e := header
	changed := false
	if !e.Finalized && e.Kind == Recording && e.CheckpointSize > 0 {
		h := sha256.New()
		read, hashErr := io.CopyN(h, f, e.CheckpointSize)
		if hashErr == nil && read == e.CheckpointSize && hex.EncodeToString(h.Sum(nil)) == e.CheckpointHash {
			if err = f.Truncate(e.CheckpointSize); err != nil {
				return err
			}
			if err = allocate(f, (e.CheckpointSize+s.unit-1)/s.unit*s.unit); err != nil {
				return err
			}
			if err = f.Sync(); err != nil {
				return err
			}
			e.Size = e.CheckpointSize
			e.Finalized = true
			e.SHA256 = e.CheckpointHash
			s.state.Sequence++
			e.Sequence = s.state.Sequence
		}
		e.CheckpointSize = 0
		e.CheckpointHash = ""
		changed = true
	}
	allocated, err := allocation(f)
	if err != nil {
		return err
	}
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	var tombstone *Entry
	if e.Finalized && stat.Size() != e.Size {
		e.Finalized = false
		s.state.Sequence++
		tombstone = &Entry{ID: e.ID, Tenant: e.Tenant, Kind: e.Kind, Sequence: s.state.Sequence, Deleted: true}
		changed = true
	}
	if e.Charged != allocated || e.Size != stat.Size() {
		changed = true
	}
	if changed {
		current := s.getEntry(e.ID, true)
		if current == nil {
			return errors.New("allocation index lost")
		}
		e.Metadata = current.Metadata
		e.Charged = allocated
		e.Size = stat.Size()
		return s.commit(ledgerChange{Entry: &e, Tombstone: tombstone})
	}
	return nil
}

// Legacy sources are retained at root/index.json.legacy and ledger.log.legacy.
// They can be restored together with the matching pre-migration media snapshot.
