package storage

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

type ledgerChange struct {
	Entry        *Entry            `json:",omitempty"`
	Deleted      string            `json:",omitempty"`
	LimitKey     string            `json:",omitempty"`
	Limit        int64             `json:",omitempty"`
	OperationKey string            `json:",omitempty"`
	Operation    reservationRecord `json:",omitempty"`
	Tombstone    *Entry            `json:",omitempty"`
	Acknowledged int64             `json:",omitempty"`
}

func bytesClone(v []byte) []byte { return append([]byte(nil), v...) }
func (s *Store) openJournal() error {
	f, err := os.OpenFile(filepath.Join(s.root, "ledger.log"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	s.journal = f
	r := bufio.NewReader(f)
	var complete int64
	for {
		var header [8]byte
		_, err = io.ReadFull(r, header[:])
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			f.Close()
			return err
		}
		n := binary.BigEndian.Uint32(header[:4])
		if n == 0 || n > 16<<20 {
			f.Close()
			return errors.New("invalid ledger record")
		}
		data := make([]byte, n)
		_, err = io.ReadFull(r, data)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			f.Close()
			return err
		}
		if crc32.ChecksumIEEE(data) != binary.BigEndian.Uint32(header[4:]) {
			f.Close()
			return errors.New("ledger checksum mismatch")
		}
		var change ledgerChange
		if err = json.Unmarshal(data, &change); err != nil {
			f.Close()
			return err
		}
		if change.Entry != nil {
			s.state.Entries[change.Entry.ID] = change.Entry
			if change.Entry.Sequence > s.state.Sequence {
				s.state.Sequence = change.Entry.Sequence
			}
		}
		if change.Deleted != "" {
			delete(s.state.Entries, change.Deleted)
		}
		if change.Tombstone != nil {
			s.state.Deleted[change.Tombstone.ID] = change.Tombstone
			if change.Tombstone.Sequence > s.state.Sequence {
				s.state.Sequence = change.Tombstone.Sequence
			}
		}
		if change.Acknowledged > 0 {
			for id, e := range s.state.Deleted {
				if e.Sequence <= change.Acknowledged {
					delete(s.state.Deleted, id)
				}
			}
		}
		if change.LimitKey != "" {
			s.state.Limits[change.LimitKey] = change.Limit
		}
		if change.OperationKey != "" {
			s.state.Operations[change.OperationKey] = change.Operation
		}
		complete += 8 + int64(n)
	}
	// A torn final record never granted permission to grow a file: callers
	// allocate only after the entire record has been synced successfully.
	if err = f.Truncate(complete); err != nil {
		f.Close()
		return err
	}
	_, err = f.Seek(complete, io.SeekStart)
	return err
}
