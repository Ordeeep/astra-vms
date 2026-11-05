// Package storage owns all file handles that can grow the video archive.
// One process holds the volume lock. Capacity is allocated before any media
// bytes are accepted; clients never receive a raw writable file handle.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type Kind string

const Recording Kind = "recording"
const Export Kind = "export"

var ErrCapacity = errors.New("storage capacity unavailable")
var ErrProtected = errors.New("recording protected by active readers")
var ErrUnavailable = errors.New("storage unavailable")
var ErrNotFound = errors.New("recording not found")
var validTenant = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

type Options struct {
	VolumeLimit int64
	Margin      int64
	// Trace is delivered after the storage operation releases its lock.
	// Events describe operation-time charges, not the state at callback time.
	Trace func(Event)
}
type Event struct {
	At        time.Time
	Operation string
	ID        string
	Charged   int64
	Total     int64
	Error     string
}
type Entry struct {
	ID             string
	Tenant         string
	Kind           Kind
	Created        time.Time
	Charged        int64
	Size           int64
	Finalized      bool
	Metadata       json.RawMessage
	Sequence       int64
	Deleted        bool
	Writing        bool `json:"-"`
	CheckpointSize int64
	CheckpointHash string
	SHA256         string
}
type reservationRecord struct {
	ID       string
	Capacity int64
}
type Reservation struct {
	ID        string
	Writer    *Writer
	Finalized bool
}
type state struct {
	Sequence   int64
	Deleted    map[string]*Entry
	Operations map[string]reservationRecord
	Limits     map[string]int64
	Entries    map[string]*Entry
}
type Store struct {
	index        *bolt.DB
	charges      map[string]int64
	totalCharge  int64
	mu           sync.Mutex
	root         string
	opts         Options
	unit         int64
	lock         *os.File
	state        state
	writers      map[string]*Writer
	readers      map[string]map[*Reader]bool
	authority    bool
	closed       bool
	fault        error
	events       []Event
	journal      *os.File
	journalCount int
}
type Writer struct {
	store    *Store
	entry    *Entry
	file     *os.File
	capacity int64
	closed   bool
	hash     hash.Hash
}
type Reader struct {
	store  *Store
	id     string
	file   *os.File
	closed bool
}

func Open(root string, opts Options) (*Store, error) {
	if opts.VolumeLimit <= 0 || opts.Margin < 0 {
		return nil, errors.New("invalid storage limits")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(root, ".lock"))
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, opts: opts, lock: lock, writers: map[string]*Writer{}, readers: map[string]map[*Reader]bool{}, authority: true}
	ready := false
	defer func() {
		if !ready {
			if s.index != nil {
				s.index.Close()
			}
			lock.Close()
		}
	}()
	s.unit, _, err = volumeInfo(root)
	if err != nil {
		return nil, err
	}
	if err = s.openIndex(); err != nil {
		return nil, err
	}
	if err = s.reconcileIndex(); err != nil {
		return nil, err
	}
	ready = true
	return s, nil
}
func key(tenant string, kind Kind) string { return tenant + "/" + string(kind) }
func (s *Store) path(id string) string    { return filepath.Join(s.root, id+".media") }
func (s *Store) usable() error {
	if s.closed || s.fault != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) total() int64                         { return s.totalCharge }
func (s *Store) usage(tenant string, kind Kind) int64 { return s.charges[key(tenant, kind)] }
func (s *Store) Usage(tenant string, kind Kind) int64 {
	s.mu.Lock()
	defer s.unlock()
	return s.usage(tenant, kind)
}
func (s *Store) AllocationUnit() int64 { return s.unit }
func (s *Store) SetLimit(tenant string, kind Kind, n int64) error {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if !validTenant.MatchString(tenant) || (kind != Recording && kind != Export) || n < 0 {
		return errors.New("invalid quota")
	}
	if s.usage(tenant, kind) > n {
		return ErrCapacity
	}
	s.state.Limits[key(tenant, kind)] = n
	return s.commit(ledgerChange{LimitKey: key(tenant, kind), Limit: n})
}
func (s *Store) SetAuthority(available bool) {
	s.mu.Lock()
	defer s.unlock()
	s.authority = available
}
func (s *Store) event(op string, e *Entry, err error) {
	if s.opts.Trace == nil {
		return
	}
	ev := Event{At: time.Now().UTC(), Operation: op, ID: e.ID, Charged: e.Charged, Total: s.total()}
	if err != nil {
		ev.Error = err.Error()
	}
	s.events = append(s.events, ev)
}
func (s *Store) Create(tenant string, kind Kind, capacity int64) (*Writer, error) {
	return s.CreateUntil(context.Background(), tenant, kind, capacity, time.Time{})
}
func (s *Store) CreateUntil(ctx context.Context, tenant string, kind Kind, capacity int64, expires time.Time) (*Writer, error) {
	s.mu.Lock()
	defer s.unlock()
	if ctx.Err() != nil || (!expires.IsZero() && !time.Now().Before(expires)) {
		return nil, ErrUnavailable
	}
	return s.create(tenant, kind, capacity, "")
}

func (s *Store) create(tenant string, kind Kind, capacity int64, operation string) (*Writer, error) {
	if err := s.usable(); err != nil {
		return nil, err
	}
	if !s.authority {
		return nil, ErrUnavailable
	}
	if !validTenant.MatchString(tenant) || (kind != Recording && kind != Export) || capacity <= 0 || capacity > 1<<40 {
		return nil, errors.New("invalid writer request")
	}
	charge := (capacity + s.unit - 1) / s.unit * s.unit
	limit, ok := s.state.Limits[key(tenant, kind)]
	if !ok || charge > limit-s.usage(tenant, kind) || charge > s.opts.VolumeLimit-s.total() {
		return nil, ErrCapacity
	}
	_, free, err := volumeInfo(s.root)
	if err != nil {
		return nil, err
	}
	if charge > free-s.opts.Margin {
		return nil, ErrCapacity
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return nil, err
	}
	e := &Entry{ID: hex.EncodeToString(token[:]), Tenant: tenant, Kind: kind, Created: time.Now().UTC(), Charged: charge}
	change := ledgerChange{Entry: e}
	if operation != "" {
		change.OperationKey = key(tenant, kind) + "/" + operation
		change.Operation = reservationRecord{ID: e.ID, Capacity: capacity}
	}
	if err = s.commit(change); err != nil {
		return nil, err
	}
	// The durable reservation exists before creation/allocation.
	s.event("reserve", e, nil)
	f, err := os.OpenFile(s.path(e.ID), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		s.fault = err
		return nil, err
	}
	err = allocate(f, charge)
	actual, measureErr := allocation(f)
	if err != nil || measureErr != nil || actual > charge {
		f.Close()
		if err == nil {
			err = fmt.Errorf("allocation cannot be bounded: requested %d actual %d measurement %v", charge, actual, measureErr)
		}
		// Do not release a potentially occupied reservation.
		s.fault = err
		return nil, err
	}
	w := &Writer{store: s, entry: e, file: f, capacity: capacity, hash: sha256.New()}
	s.writers[e.ID] = w
	s.event("allocate", e, nil)
	return w, nil
}

// CreateCapture alone may reclaim recordings. Export admission never evicts
// history. The same volume lock still serializes the eventual allocation.
func (s *Store) CreateCapture(tenant string, capacity int64) (*Writer, error) {
	return s.CreateCaptureUntil(context.Background(), tenant, capacity, time.Time{})
}
func (s *Store) CreateCaptureUntil(ctx context.Context, tenant string, capacity int64, expires time.Time) (*Writer, error) {
	s.mu.Lock()
	defer s.unlock()
	for {
		if ctx.Err() != nil || (!expires.IsZero() && !time.Now().Before(expires)) {
			return nil, ErrUnavailable
		}
		w, err := s.create(tenant, Recording, capacity, "")
		if !errors.Is(err, ErrCapacity) {
			return w, err
		}
		charge := (capacity + s.unit - 1) / s.unit * s.unit
		limit := s.state.Limits[key(tenant, Recording)]
		if charge > limit || charge > s.opts.VolumeLimit {
			return nil, ErrCapacity
		}
		ownPressure := charge > limit-s.usage(tenant, Recording)
		oldest := s.oldestCaptureCandidate(tenant, ownPressure)
		if oldest == nil {
			return nil, ErrCapacity
		}
		if err := s.delete(oldest.Tenant, oldest.ID, true); err != nil {
			return nil, err
		}
	}
}

func (s *Store) oldestCaptureCandidate(tenant string, ownPressure bool) *Entry {
	if !ownPressure {
		tenant = ""
	}
	return s.oldestRecording(tenant, true)
}

// Grow performs admission and physical preallocation before allowing more
// writes. Only a recording capture may preempt readers and evict history.
func (w *Writer) Grow(capacity int64, forCapture bool) error {
	return w.GrowUntil(context.Background(), capacity, forCapture, time.Time{})
}
func (w *Writer) GrowUntil(ctx context.Context, capacity int64, forCapture bool, expires time.Time) error {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if !s.authority {
		return ErrUnavailable
	}
	if ctx.Err() != nil || (!expires.IsZero() && !time.Now().Before(expires)) {
		return ErrUnavailable
	}
	if w.closed {
		return os.ErrClosed
	}
	if capacity <= w.capacity {
		return nil
	}
	if capacity > 1<<40 {
		return ErrCapacity
	}
	charge := (capacity + s.unit - 1) / s.unit * s.unit
	extra := charge - w.entry.Charged
	for {
		if ctx.Err() != nil || (!expires.IsZero() && !time.Now().Before(expires)) {
			return ErrUnavailable
		}
		limit := s.state.Limits[key(w.entry.Tenant, w.entry.Kind)]
		_, free, err := volumeInfo(s.root)
		if err != nil {
			return err
		}
		own := extra > limit-s.usage(w.entry.Tenant, w.entry.Kind)
		if !own && extra <= s.opts.VolumeLimit-s.total() && extra <= free-s.opts.Margin {
			break
		}
		if !forCapture || w.entry.Kind != Recording || charge > limit || charge > s.opts.VolumeLimit {
			return ErrCapacity
		}
		oldest := s.oldestCaptureCandidate(w.entry.Tenant, own)
		if oldest == nil {
			return ErrCapacity
		}
		if err = s.delete(oldest.Tenant, oldest.ID, true); err != nil {
			return err
		}
	}
	w.entry.Charged = charge
	if err := s.commit(ledgerChange{Entry: w.entry}); err != nil {
		return err
	}
	if err := allocate(w.file, charge); err != nil {
		s.fault = err
		return err
	}
	actual, err := allocation(w.file)
	if err != nil || actual > charge {
		s.fault = ErrUnavailable
		return ErrUnavailable
	}
	w.capacity = capacity
	s.event("grow", w.entry, nil)
	return nil
}

func (w *Writer) ID() string { return w.entry.ID }
func (w *Writer) Write(p []byte) (int, error) {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return 0, err
	}
	if w.closed {
		return 0, os.ErrClosed
	}
	if int64(len(p)) > w.capacity-w.entry.Size {
		return 0, ErrCapacity
	}
	s.event("before-write", w.entry, nil)
	n, err := w.file.Write(p)
	w.hash.Write(p[:n])
	w.entry.Size += int64(n)
	s.event("after-write", w.entry, err)
	if err != nil {
		s.fault = err
	}
	return n, err
}
func (w *Writer) Finish() error { return w.FinishWithMetadata(nil) }

// Checkpoint is called only after a complete independently decodable fragment.
// Media bytes are flushed before the durable metadata/hash commit. Recovery
// truncates any later, uncommitted tail before publishing the object.
func (w *Writer) Checkpoint(metadata json.RawMessage) error {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if w.closed {
		return os.ErrClosed
	}
	if w.entry.Kind != Recording || w.entry.Size == 0 || !json.Valid(metadata) {
		return errors.New("invalid recording checkpoint")
	}
	if err := w.file.Sync(); err != nil {
		s.fault = err
		return err
	}
	w.entry.Metadata = bytesClone(metadata)
	w.entry.CheckpointSize = w.entry.Size
	w.entry.CheckpointHash = hex.EncodeToString(w.hash.Sum(nil))
	return s.commit(ledgerChange{Entry: w.entry})
}
func (w *Writer) Tag(metadata json.RawMessage) error {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if w.closed {
		return os.ErrClosed
	}
	w.entry.Metadata = bytesClone(metadata)
	return s.commit(ledgerChange{Entry: w.entry})
}

// Abort releases the write handle without advertising an incomplete object.
// Its allocation remains charged until a successful physical deletion.
func (w *Writer) Abort() error {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if w.closed {
		return nil
	}
	err := w.file.Close()
	w.closed = true
	delete(s.writers, w.entry.ID)
	if err != nil {
		return err
	}
	return s.commit(ledgerChange{Entry: w.entry})
}
func (w *Writer) FinishWithMetadata(metadata json.RawMessage) error {
	s := w.store
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if w.closed {
		return os.ErrClosed
	}
	if err := w.file.Sync(); err != nil {
		s.fault = err
		return err
	}
	if err := w.file.Truncate(w.entry.Size); err != nil {
		s.fault = err
		return err
	}
	if err := allocate(w.file, (w.entry.Size+s.unit-1)/s.unit*s.unit); err != nil {
		s.fault = err
		return err
	}
	n, err := allocation(w.file)
	if err != nil {
		s.fault = err
		return err
	}
	if err = w.file.Close(); err != nil {
		s.fault = err
		return err
	}
	w.closed = true
	delete(s.writers, w.entry.ID)
	w.entry.Charged = n
	w.entry.Finalized = true
	w.entry.SHA256 = hex.EncodeToString(w.hash.Sum(nil))
	w.entry.CheckpointSize = 0
	w.entry.CheckpointHash = ""
	w.entry.Metadata = bytesClone(metadata)
	s.state.Sequence++
	w.entry.Sequence = s.state.Sequence
	if err = s.commit(ledgerChange{Entry: w.entry}); err != nil {
		return err
	}
	s.event("finish", w.entry, nil)
	return nil
}
func (s *Store) Entry(tenant, id string) (Entry, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return Entry{}, err
	}
	e := s.getEntry(id, true)
	if s.fault != nil {
		return Entry{}, ErrUnavailable
	}
	if e == nil || e.Tenant != tenant {
		return Entry{}, ErrNotFound
	}
	copy := *e
	copy.Metadata = bytesClone(e.Metadata)
	return copy, nil
}
func (s *Store) Catalog(after int64, limit int) []Entry {
	s.mu.Lock()
	defer s.unlock()
	if s.usable() != nil {
		return nil
	}
	return s.catalogPage(after, limit)
}
func (s *Store) CatalogPage(after int64, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return nil, err
	}
	if after < 0 || after == int64(^uint64(0)>>1) || limit < 1 || limit > 100 {
		return nil, errors.New("invalid catalogue page")
	}
	items := s.catalogPage(after, limit)
	if s.fault != nil {
		return nil, ErrUnavailable
	}
	return items, nil
}
func (s *Store) Acknowledge(sequence int64) error {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if sequence < 0 || sequence > s.state.Sequence {
		return errors.New("invalid catalogue acknowledgement")
	}
	return s.commit(ledgerChange{Acknowledged: sequence})
}

// Entries is the bounded first page retained for diagnostic/test callers.
// Production scans use ObjectsPage with an explicit cursor.
func (s *Store) Entries(tenant string, kind Kind) []Entry {
	items, _, _ := s.ObjectsPage(tenant, kind, "", 100)
	return items
}
func (s *Store) Read(tenant, id string) (*Reader, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return nil, err
	}
	e := s.getEntry(id, true)
	if e == nil || e.Tenant != tenant || !e.Finalized {
		return nil, ErrNotFound
	}
	f, err := os.Open(s.path(id))
	if err != nil {
		return nil, err
	}
	r := &Reader{store: s, id: id, file: f}
	if s.readers[id] == nil {
		s.readers[id] = map[*Reader]bool{}
	}
	s.readers[id][r] = true
	return r, nil
}
func (r *Reader) Read(p []byte) (int, error) {
	s := r.store
	s.mu.Lock()
	defer s.unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	return r.file.Read(p)
}
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	s := r.store
	s.mu.Lock()
	defer s.unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	return r.file.Seek(offset, whence)
}
func (r *Reader) close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	delete(r.store.readers[r.id], r)
	if len(r.store.readers[r.id]) == 0 {
		delete(r.store.readers, r.id)
	}
	return r.file.Close()
}
func (r *Reader) Close() error { r.store.mu.Lock(); defer r.store.mu.Unlock(); return r.close() }
func (s *Store) Delete(tenant, id string, forCapture bool) error {
	s.mu.Lock()
	defer s.unlock()
	return s.delete(tenant, id, forCapture)
}

func (s *Store) delete(tenant, id string, forCapture bool) error {
	if err := s.usable(); err != nil {
		return err
	}
	e := s.getEntry(id, true)
	if e == nil || e.Tenant != tenant {
		return ErrNotFound
	}
	if s.writers[id] != nil {
		return ErrProtected
	}
	if len(s.readers[id]) > 0 && !forCapture {
		return ErrProtected
	}
	for r := range s.readers[id] {
		if err := r.close(); err != nil {
			return err
		}
	}
	if err := os.Remove(s.path(id)); err != nil {
		s.event("delete-failed", e, err)
		return err
	}
	delete(s.readers, id)
	s.state.Sequence++
	tombstone := &Entry{ID: id, Tenant: e.Tenant, Kind: e.Kind, Sequence: s.state.Sequence, Deleted: true}
	if err := s.commit(ledgerChange{Deleted: id, Tombstone: tombstone}); err != nil {
		return err
	}
	s.event("delete", e, nil)
	return nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	for _, w := range s.writers {
		result = errors.Join(result, w.file.Close())
		w.closed = true
	}
	for _, readers := range s.readers {
		for r := range readers {
			result = errors.Join(result, r.close())
		}
	}
	result = errors.Join(result, s.index.Close(), s.lock.Close())
	return result
}

func (s *Store) unlock() {
	events := s.events
	s.events = nil
	s.mu.Unlock()
	if s.opts.Trace != nil {
		for _, event := range events {
			s.opts.Trace(event)
		}
	}
}

// CreateOperation is the retryable command boundary. Retrying a completed or
// retired command never creates another writable allocation.
func (s *Store) CreateOperation(tenant string, kind Kind, capacity int64, operation string) (Reservation, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return Reservation{}, err
	}
	if !validTenant.MatchString(operation) {
		return Reservation{}, errors.New("invalid operation identity")
	}
	var record reservationRecord
	found := false
	if err := s.index.View(func(tx *bolt.Tx) error {
		raw := bucket(tx, "operations").Get([]byte(key(tenant, kind) + "/" + operation))
		if raw == nil {
			return nil
		}
		found = true
		return json.Unmarshal(raw, &record)
	}); err != nil {
		s.fault = err
		return Reservation{}, err
	}
	if found {
		if record.Capacity != capacity {
			return Reservation{}, errors.New("operation content conflict")
		}
		e := s.getEntry(record.ID, false)
		if e == nil {
			return Reservation{ID: record.ID}, ErrNotFound
		}
		return Reservation{ID: e.ID, Writer: s.writers[e.ID], Finalized: e.Finalized}, nil
	}
	w, err := s.create(tenant, kind, capacity, operation)
	if err != nil {
		return Reservation{}, err
	}
	return Reservation{ID: w.ID(), Writer: w}, nil
}
