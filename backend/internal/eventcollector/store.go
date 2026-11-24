package eventcollector

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"vms.local/backend/internal/camera"
)

type Rule struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Schedule string `json:"schedule,omitempty"`
}
type Assignment struct {
	Tenant   string        `json:"tenant"`
	Camera   string        `json:"camera"`
	SourceID string        `json:"sourceId"`
	Revision int64         `json:"revision"`
	Source   camera.Source `json:"source"`
	Rules    []Rule        `json:"rules"`
}
type Record struct {
	ID        string              `json:"id"`
	Tenant    string              `json:"tenant"`
	Camera    string              `json:"camera"`
	Revision  int64               `json:"revision"`
	At        time.Time           `json:"at"`
	Alert     *camera.CameraAlert `json:"alert,omitempty"`
	Condition string              `json:"condition,omitempty"`
	Code      string              `json:"code,omitempty"`
}
type Batch struct {
	Items    []Record `json:"items"`
	Pressure bool     `json:"pressure"`
	Bytes    int64    `json:"bytes"`
}
type Store struct {
	db           *bolt.DB
	crypt        cipher.AEAD
	capacity     int64
	reservations map[string]int64
}

var full = errors.New("tenant_event_queue_full")
var buckets = []string{"config", "queue", "usage", "state", "last"}

func Open(dir, secret string, capacity int64, reservations map[string]int64) (*Store, error) {
	if len(secret) < 32 || capacity < 1 {
		return nil, errors.New("invalid collector configuration")
	}
	var allocated int64
	for _, n := range reservations {
		if n <= 0 || n > capacity-allocated {
			return nil, errors.New("invalid tenant reservation")
		}
		allocated += n
	}
	key := sha256.Sum256([]byte("astra-event-assignments\x00" + secret))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	db, e := bolt.Open(filepath.Join(dir, "collector.db"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	e = db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db, crypt: aead, capacity: capacity, reservations: reservations}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) assignments() ([]Assignment, error) {
	var items []Assignment
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte("config")).Get([]byte("assignments"))
		if raw == nil {
			return nil
		}
		n := s.crypt.NonceSize()
		if len(raw) < n {
			return errors.New("invalid assignment cache")
		}
		plain, e := s.crypt.Open(nil, raw[:n], raw[n:], nil)
		if e != nil {
			return errors.New("could not open cached assignments")
		}
		return json.Unmarshal(plain, &items)
	})
	return items, err
}
func (s *Store) configure(items []Assignment) error {
	plain, e := json.Marshal(items)
	if e != nil {
		return e
	}
	nonce := make([]byte, s.crypt.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	sealed := s.crypt.Seal(nonce, nonce, plain, nil)
	return s.db.Update(func(tx *bolt.Tx) error {
		cfg := tx.Bucket([]byte("config"))
		if err := cfg.Put([]byte("assignments"), sealed); err != nil {
			return err
		}
		tenants := map[string]bool{}
		for _, a := range items {
			tenants[a.Tenant] = true
		}
		usage := tx.Bucket([]byte("usage"))
		var sharedUsed int64
		if err := usage.ForEach(func(k, v []byte) error {
			if _, fixed := s.reservations[string(k)]; !fixed {
				sharedUsed += number(v)
			}
			if number(v) > 0 {
				tenants[string(k)] = true
			}
			return nil
		}); err != nil {
			return err
		}
		remaining := s.capacity
		for _, n := range s.reservations {
			remaining -= n
		}
		if err := cfg.Put([]byte("sharedUsed"), bin(sharedUsed)); err != nil {
			return err
		}
		if err := cfg.Put([]byte("sharedLimit"), bin(remaining)); err != nil {
			return err
		}
		count := 0
		for tenant := range tenants {
			if _, fixed := s.reservations[tenant]; !fixed {
				count++
			}
		}
		limits := map[string]int64{}
		for tenant := range tenants {
			if n, fixed := s.reservations[tenant]; fixed {
				limits[tenant] = n
			} else {
				limits[tenant] = remaining / int64(count)
			}
		}
		encoded, err := json.Marshal(limits)
		if err != nil {
			return err
		}
		return cfg.Put([]byte("limits"), encoded)
	})
}
func number(v []byte) int64 {
	if len(v) != 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(v))
}
func bin(n int64) []byte { return binary.BigEndian.AppendUint64(nil, uint64(n)) }
func itemKey(a Assignment, condition string) []byte {
	return []byte(a.Tenant + "/" + a.Camera + "/" + fmt.Sprint(a.Revision) + "/" + condition)
}
func identifier() string { var id [24]byte; rand.Read(id[:]); return hex.EncodeToString(id[:]) }
func (s *Store) append(tx *bolt.Tx, r Record, limited bool) error {
	r.ID = identifier()
	data, e := json.Marshal(r)
	if e != nil {
		return e
	}
	usage := tx.Bucket([]byte("usage"))
	t := []byte(r.Tenant)
	used := number(usage.Get(t))
	size := int64(len(data) + 64)
	cfg := tx.Bucket([]byte("config"))
	_, fixed := s.reservations[r.Tenant]
	sharedUsed := number(cfg.Get([]byte("sharedUsed")))
	if limited {
		var limits map[string]int64
		if e = json.Unmarshal(tx.Bucket([]byte("config")).Get([]byte("limits")), &limits); e != nil {
			return e
		}
		if used+size > limits[r.Tenant] || (!fixed && sharedUsed+size > number(cfg.Get([]byte("sharedLimit")))) {
			return full
		}
	}
	q := tx.Bucket([]byte("queue"))
	seq, e := q.NextSequence()
	if e != nil {
		return e
	}
	if e = q.Put(bin(int64(seq)), data); e != nil {
		return e
	}
	if !fixed {
		if err := cfg.Put([]byte("sharedUsed"), bin(sharedUsed+size)); err != nil {
			return err
		}
	}
	return usage.Put(t, bin(used+size))
}
func (s *Store) transition(tx *bolt.Tx, a Assignment, condition, code string, at time.Time) error {
	b := tx.Bucket([]byte("state"))
	key := itemKey(a, condition)
	if string(b.Get(key)) == code {
		return nil
	}
	r := Record{Tenant: a.Tenant, Camera: a.Camera, Revision: a.Revision, At: at.UTC(), Condition: condition, Code: code}
	// Reserve space for one saturation notice per affected session even if its
	// tenant has exhausted the payload budget. Other transitions share the cap.
	if e := s.append(tx, r, condition != "queue"); e != nil {
		if errors.Is(e, full) {
			return s.transition(tx, a, "queue", "queue_full", at)
		}
		return e
	}
	if code == "" {
		return b.Delete(key)
	}
	return b.Put(key, []byte(code))
}
func (s *Store) signal(a Assignment, condition, code string, at time.Time) error {
	return s.db.Update(func(tx *bolt.Tx) error { return s.transition(tx, a, condition, code, at) })
}
func (s *Store) stopped(a Assignment) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := s.append(tx, Record{Tenant: a.Tenant, Camera: a.Camera, Revision: a.Revision, At: time.Now().UTC(), Condition: "lifecycle", Code: "stopped"}, false); err != nil {
			return err
		}
		prefix := itemKey(a, "")
		state := tx.Bucket([]byte("state"))
		cursor := state.Cursor()
		for k, _ := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cursor.Seek(prefix) {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
		return tx.Bucket([]byte("last")).Delete(prefix)
	})
}
func (s *Store) last(a Assignment) time.Time {
	var ns int64
	s.db.View(func(tx *bolt.Tx) error { ns = number(tx.Bucket([]byte("last")).Get(itemKey(a, ""))); return nil })
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}
func (s *Store) observe(a Assignment, alert *camera.CameraAlert, at time.Time) (bool, error) {
	saturated := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := s.transition(tx, a, "stream", "", at); err != nil {
			return err
		}
		last := tx.Bucket([]byte("last"))
		key := itemKey(a, "")
		if alert != nil || at.UnixNano()-number(last.Get(key)) >= int64(5*time.Second) {
			if err := last.Put(key, bin(at.UnixNano())); err != nil {
				return err
			}
		}
		if alert == nil {
			return nil
		}
		e := s.append(tx, Record{Tenant: a.Tenant, Camera: a.Camera, Revision: a.Revision, At: at.UTC(), Alert: alert}, true)
		if errors.Is(e, full) {
			saturated = tx.Bucket([]byte("state")).Get(itemKey(a, "queue")) == nil
			return s.transition(tx, a, "queue", "queue_full", at)
		}
		if e != nil {
			return e
		}
		return s.transition(tx, a, "queue", "", at)
	})
	return saturated, err
}
func (s *Store) batch() (Batch, error) {
	var out Batch
	e := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte("queue")).Cursor()
		for k, v := c.First(); k != nil && len(out.Items) < 128; k, v = c.Next() {
			var r Record
			if e := json.Unmarshal(v, &r); e != nil {
				return e
			}
			out.Items = append(out.Items, r)
		}
		var limits map[string]int64
		if raw := tx.Bucket([]byte("config")).Get([]byte("limits")); raw != nil {
			if e := json.Unmarshal(raw, &limits); e != nil {
				return e
			}
		}
		return tx.Bucket([]byte("usage")).ForEach(func(k, v []byte) error {
			used := number(v)
			out.Bytes += used
			if used > 0 && used >= limits[string(k)]*8/10 {
				out.Pressure = true
			}
			return nil
		})
	})
	return out, e
}
func (s *Store) ack(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		q := tx.Bucket([]byte("queue"))
		u := tx.Bucket([]byte("usage"))
		for _, id := range ids {
			k, v := q.Cursor().First()
			if k == nil {
				return errors.New("stale acknowledgement")
			}
			var r Record
			if e := json.Unmarshal(v, &r); e != nil {
				return e
			}
			if r.ID != id {
				return errors.New("stale acknowledgement")
			}
			n := int64(len(v) + 64)
			if e := q.Delete(bytes.Clone(k)); e != nil {
				return e
			}
			tenant := []byte(r.Tenant)
			if _, fixed := s.reservations[r.Tenant]; !fixed {
				cfg := tx.Bucket([]byte("config"))
				if err := cfg.Put([]byte("sharedUsed"), bin(number(cfg.Get([]byte("sharedUsed")))-n)); err != nil {
					return err
				}
			}
			if e := u.Put(tenant, bin(number(u.Get(tenant))-n)); e != nil {
				return e
			}
		}
		return s.closeDrainedQueues(tx)
	})
}

func (s *Store) closeDrainedQueues(tx *bolt.Tx) error {
	var limits map[string]int64
	if raw := tx.Bucket([]byte("config")).Get([]byte("limits")); raw != nil {
		if err := json.Unmarshal(raw, &limits); err != nil {
			return err
		}
	}
	var recovered []Assignment
	states := tx.Bucket([]byte("state"))
	err := states.ForEach(func(k, v []byte) error {
		if string(v) != "queue_full" {
			return nil
		}
		parts := strings.Split(string(k), "/")
		if len(parts) != 4 {
			return nil
		}
		used := number(tx.Bucket([]byte("usage")).Get([]byte(parts[0])))
		if used > 0 && used >= limits[parts[0]]*8/10 {
			return nil
		}
		revision, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return err
		}
		recovered = append(recovered, Assignment{Tenant: parts[0], Camera: parts[1], Revision: revision})
		return nil
	})
	if err != nil {
		return err
	}
	for _, a := range recovered {
		if err := s.transition(tx, a, "queue", "", time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}
