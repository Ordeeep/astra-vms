package eventcollector

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"reflect"
	"sync"
	"time"

	"vms.local/backend/internal/camera"
)

type session struct {
	config Assignment
	cancel context.CancelFunc
	done   chan struct{}
}
type Worker struct {
	ctx         context.Context
	store       *Store
	mu          sync.Mutex
	sessions    map[string]session
	assignments []Assignment
}

func Start(ctx context.Context, s *Store) (*Worker, error) {
	items, err := s.assignments()
	if err != nil {
		return nil, err
	}
	worker := &Worker{ctx: ctx, store: s, sessions: map[string]session{}}
	if err = worker.apply(items); err != nil {
		return nil, err
	}
	return worker, nil
}
func (w *Worker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.sessions {
		s.cancel()
	}
	for _, s := range w.sessions {
		<-s.done
	}
	clear(w.sessions)
}
func (w *Worker) apply(items []Assignment) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if reflect.DeepEqual(w.assignments, items) {
		return nil
	}
	next := map[string]Assignment{}
	for _, a := range items {
		key := a.Tenant + "/" + a.Camera
		if _, dup := next[key]; dup || a.Tenant == "" || a.Camera == "" || a.Source.Connection == nil {
			return errors.New("invalid assignment")
		}
		if _, ok := camera.Events(a.Source.Type); !ok {
			return errors.New("unsupported event integration")
		}
		next[key] = a
	}
	if err := w.store.configure(items); err != nil {
		return err
	}
	for key, s := range w.sessions {
		if a, ok := next[key]; ok && reflect.DeepEqual(a, s.config) {
			continue
		}
		s.cancel()
		<-s.done
		if err := w.store.stopped(s.config); err != nil {
			return err
		}
		delete(w.sessions, key)
	}
	for key, a := range next {
		if _, ok := w.sessions[key]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(w.ctx)
		s := session{a, cancel, make(chan struct{})}
		w.sessions[key] = s
		go func() { defer close(s.done); w.run(ctx, a) }()
	}
	w.assignments = items
	return nil
}
func (w *Worker) run(ctx context.Context, a Assignment) {
	receiver, _ := camera.Events(a.Source.Type)
	rulesDone := make(chan struct{})
	go func() { defer close(rulesDone); w.monitorRules(ctx, a, receiver) }()
	defer func() { <-rulesDone }()
	last := w.store.last(a)
	resuming := !last.IsZero()
	if last.IsZero() {
		last = time.Now().UTC()
	}
	signal := func() {
		if err := w.store.signal(a, "stream", "coverage_uncertain", last); err != nil {
			log.Print("event collector could not persist channel interruption")
		}
	}
	if resuming {
		signal()
	}
	delay := time.Second
	for ctx.Err() == nil {
		healthy := false
		err := receiver.Receive(ctx, a.Source, func(alert *camera.CameraAlert) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			now := time.Now().UTC()
			saturated, err := w.store.observe(a, alert, now)
			if err != nil {
				log.Print("event collector local write failed")
				return err
			}
			if saturated {
				log.Printf("event queue saturated for tenant %s", a.Tenant)
			}
			last = now
			healthy = true
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		signal()
		if healthy {
			delay = time.Second
		}
		if err != nil && err.Error() == "camera_auth_failed" {
			delay = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay + time.Duration(rand.IntN(500))*time.Millisecond):
		}
		delay = min(60*time.Second, delay*2)
	}
}
func (w *Worker) monitorRules(ctx context.Context, a Assignment, receiver camera.EventReceiver) {
	if len(a.Rules) == 0 {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		for _, kind := range []string{"intrusion", "line_crossing"} {
			var rules []Rule
			for _, r := range a.Rules {
				if r.Kind == kind {
					rules = append(rules, r)
				}
			}
			if len(rules) == 0 {
				continue
			}
			inv, e := receiver.RuleInventory(ctx, a.Source, kind)
			for _, r := range rules {
				if ctx.Err() != nil {
					return
				}
				code := ""
				switch {
				case e != nil || inv.Enabled == nil:
					code = "verification_unavailable"
				case !*inv.Enabled:
					code = "channel_disabled"
				case r.ID == "": // The operator monitors the channel itself.
				default:
					code = "channel_only"
					if inv.Complete {
						code = "rule_missing"
					}
					for _, item := range inv.Items {
						if item.ID == r.ID && item.Enabled != nil {
							code = ""
							if !*item.Enabled {
								code = "rule_disabled"
							}
							break
						}
					}
				}
				if err := w.store.signal(a, "rule/"+kind+"/"+r.ID, code, time.Now().UTC()); err != nil {
					log.Print("event collector could not persist rule observation")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) Handler(secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /config", func(out http.ResponseWriter, r *http.Request) {
		var items []Assignment
		if !workerBody(out, r, &items) {
			return
		}
		if err := w.apply(items); err != nil {
			http.Error(out, "configuration unavailable", 503)
			return
		}
		out.WriteHeader(204)
	})
	mux.HandleFunc("GET /batch", func(out http.ResponseWriter, r *http.Request) {
		b, e := w.store.batch()
		if e != nil {
			http.Error(out, "queue unavailable", 503)
			return
		}
		out.Header().Set("Content-Type", "application/json")
		json.NewEncoder(out).Encode(b)
	})
	mux.HandleFunc("POST /ack", func(out http.ResponseWriter, r *http.Request) {
		var ids []string
		if !workerBody(out, r, &ids) {
			return
		}
		if len(ids) > 128 || w.store.ack(ids) != nil {
			http.Error(out, "invalid acknowledgement", 409)
			return
		}
		out.WriteHeader(204)
	})
	return http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		out.Header().Set("Cache-Control", "no-store")
		if len(secret) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(out, "unauthorized", 401)
			return
		}
		mux.ServeHTTP(out, r)
	})
}
func workerBody(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", 400)
		return false
	}
	return true
}
