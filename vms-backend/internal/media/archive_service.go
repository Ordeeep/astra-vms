package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vms.local/backend/internal/storage"
)

type ArchiveConfig struct {
	Path        string `json:"path"`
	VolumeLimit int64  `json:"volumeLimit"`
	Margin      int64  `json:"margin"`
}
type CapturePlan struct {
	ConnectionRevision int64     `json:"connectionRevision"`
	ID                 string    `json:"id"`
	Tenant             string    `json:"tenant"`
	Camera             string    `json:"camera"`
	Site               string    `json:"site"`
	Source             string    `json:"source"`
	Profile            string    `json:"profile"`
	URL                string    `json:"url"`
	Expires            time.Time `json:"expires"`
}
type PartIndex struct {
	ArchivePart
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}
type SegmentIndex struct {
	Tenant     string      `json:"tenant"`
	Camera     string      `json:"camera"`
	Site       string      `json:"site"`
	Source     string      `json:"source"`
	Profile    string      `json:"profile"`
	Plan       string      `json:"plan"`
	Start      time.Time   `json:"start"`
	End        time.Time   `json:"end"`
	InitLength int64       `json:"initLength"`
	Parts      []PartIndex `json:"parts"`
}
type CaptureStatus struct {
	ID        string    `json:"id"`
	Camera    string    `json:"camera"`
	LastFrame time.Time `json:"lastFrame"`
	LastWrite time.Time `json:"lastWrite"`
	Code      string    `json:"code"`
}
type capture struct {
	plan   CapturePlan
	cancel context.CancelFunc
	status CaptureStatus
	done   chan struct{}
}
type ArchiveService struct {
	mu         sync.Mutex
	config     ArchiveConfig
	configPath string
	store      *storage.Store
	captures   map[string]*capture
	exports    map[string]*exportTask
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	compatible *compatibilityCache
}

func NewArchiveService(ctx context.Context, data string) (*ArchiveService, error) {
	run, cancel := context.WithCancel(ctx)
	a := &ArchiveService{compatible: newCompatibilityCache(), configPath: filepath.Join(data, "archive-config.json"), captures: map[string]*capture{}, exports: map[string]*exportTask{}, ctx: run, cancel: cancel}
	raw, err := os.ReadFile(a.configPath)
	if err == nil {
		if json.Unmarshal(raw, &a.config) != nil {
			cancel()
			return nil, errors.New("invalid archive configuration")
		}
		a.store, err = storage.Open(a.config.Path, storage.Options{VolumeLimit: a.config.VolumeLimit, Margin: a.config.Margin})
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	if a.store != nil {
		a.cleanExportArtifacts(true)
	}
	a.wg.Add(1)
	go a.monitor()
	return a, nil
}
func (a *ArchiveService) Close() {
	a.cancel()
	a.mu.Lock()
	for _, c := range a.captures {
		c.cancel()
	}
	a.mu.Unlock()
	a.wg.Wait()
	if a.store != nil {
		a.store.Close()
	}
}
func (a *ArchiveService) monitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	cycles := 0
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			a.mu.Lock()
			for _, c := range a.captures {
				if !now.Before(c.plan.Expires) {
					c.cancel()
					c.status.Code = "authority_expired"
				}
			}
			for _, job := range a.exports {
				if job.status.Status == "running" && !now.Before(job.expires) {
					job.cancel()
				}
			}
			for id, job := range a.exports {
				if !job.finished.IsZero() && now.Sub(job.finished) > time.Minute {
					delete(a.exports, id)
				}
			}
			a.mu.Unlock()
			cycles++
			if cycles%100 == 0 {
				a.cleanExportArtifacts(false)
			}
		}
	}
}
func (a *ArchiveService) configure(config ArchiveConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		if config != a.config {
			return errors.New("archive path already configured; storage migration requires a separate operation")
		}
		return nil
	}
	if !filepath.IsAbs(config.Path) || strings.HasPrefix(config.Path, `\\`) || config.VolumeLimit < 1<<20 || config.Margin < 0 {
		return errors.New("invalid local storage configuration")
	}
	store, err := storage.Open(config.Path, storage.Options{VolumeLimit: config.VolumeLimit, Margin: config.Margin})
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(config)
	if err = os.WriteFile(a.configPath, raw, 0600); err != nil {
		store.Close()
		return err
	}
	a.store, a.config = store, config
	return nil
}
func (a *ArchiveService) apply(plan CapturePlan) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil || a.store == nil {
		return errors.New("archive unavailable")
	}
	if plan.ID == "" || plan.Tenant == "" || plan.Camera == "" || plan.Site == "" || plan.Source == "" || plan.Profile == "" || !time.Now().Before(plan.Expires) || time.Until(plan.Expires) > 1500*time.Millisecond {
		return errors.New("invalid capture plan")
	}
	if c := a.captures[plan.ID]; c != nil {
		if c.plan.ConnectionRevision != plan.ConnectionRevision || c.plan.Tenant != plan.Tenant || c.plan.Camera != plan.Camera || c.plan.Source != plan.Source || c.plan.Site != plan.Site || c.plan.Profile != plan.Profile || c.plan.URL != plan.URL || !time.Now().Before(c.plan.Expires) {
			return errors.New("plan conflict or expired authority")
		}
		c.plan.Expires = plan.Expires
		return nil
	}
	if err := a.store.ClaimCapturePlan(plan.ID); err != nil {
		return err
	}
	// Only one plan can produce a camera's recording at any time.
	for _, c := range a.captures {
		if c.plan.Tenant == plan.Tenant && c.plan.Camera == plan.Camera {
			c.cancel()
		}
	}
	ctx, cancel := context.WithCancel(a.ctx)
	c := &capture{plan: plan, cancel: cancel, done: make(chan struct{}), status: CaptureStatus{ID: plan.ID, Camera: plan.Camera, Code: "connecting"}}
	a.captures[plan.ID] = c
	a.wg.Add(1)
	go a.record(ctx, c)
	return nil
}
func (a *ArchiveService) record(ctx context.Context, c *capture) {
	defer a.wg.Done()
	defer close(c.done)
	var writer *storage.Writer
	var index SegmentIndex
	var init []byte
	var offset int64
	finish := func() error {
		if writer == nil {
			return nil
		}
		raw, _ := json.Marshal(index)
		err := writer.FinishWithMetadata(raw)
		writer = nil
		return err
	}
	defer func() { finish(); a.mu.Lock(); delete(a.captures, c.plan.ID); a.mu.Unlock() }()
	for ctx.Err() == nil {
		err := ReceiveArchive(ctx, c.plan.URL, func(p ArchivePart) error {
			a.mu.Lock()
			valid := ctx.Err() == nil && time.Now().Before(c.plan.Expires)
			expires := c.plan.Expires
			a.mu.Unlock()
			if !valid {
				return errors.New("authority_expired")
			}
			if writer != nil && (p.Start.Sub(index.Start) >= 30*time.Second || !bytes.Equal(init, p.Init) || !p.Start.Equal(index.End)) {
				if err := finish(); err != nil {
					return err
				}
			}
			if writer == nil {
				var err error
				writer, err = a.store.CreateCaptureUntil(ctx, c.plan.Tenant, int64(len(p.Init)+len(p.Data)), expires)
				if err != nil {
					return err
				}
				init = bytes.Clone(p.Init)
				index = SegmentIndex{Tenant: c.plan.Tenant, Camera: c.plan.Camera, Site: c.plan.Site, Source: c.plan.Source, Profile: c.plan.Profile, Plan: c.plan.ID, Start: p.Start, InitLength: int64(len(init))}
				if _, err = writer.Write(init); err != nil {
					return err
				}
				offset = int64(len(init))
			} else if err := writer.GrowUntil(ctx, offset+int64(len(p.Data)), true, expires); err != nil {
				return err
			}
			if _, err := writer.Write(p.Data); err != nil {
				return err
			}
			index.Parts = append(index.Parts, PartIndex{ArchivePart: p, Offset: offset, Length: int64(len(p.Data))})
			index.End = p.End
			offset += int64(len(p.Data))
			// Data/init buffers must not be retained by a long segment.
			index.Parts[len(index.Parts)-1].Init = nil
			index.Parts[len(index.Parts)-1].Data = nil
			checkpoint, _ := json.Marshal(index)
			if err := writer.Checkpoint(checkpoint); err != nil {
				return err
			}
			a.mu.Lock()
			c.status.LastWrite = time.Now().UTC()
			c.status.Code = "recording"
			a.mu.Unlock()
			return nil
		}, func(at time.Time) { a.mu.Lock(); c.status.LastFrame = at; a.mu.Unlock() })
		if finishErr := finish(); finishErr != nil {
			err = finishErr
		}
		a.mu.Lock()
		c.status.Code = "source_unavailable"
		if errors.Is(err, storage.ErrCapacity) {
			c.status.Code = "storage_capacity"
		}
		if errors.Is(err, storage.ErrUnavailable) {
			c.status.Code = "storage_unavailable"
		}
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func archiveJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		json.NewEncoder(w).Encode(value)
	}
}
func (a *ArchiveService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Mounted only after Guard's private service authentication.
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.URL.Path == "/archive/config" && r.Method == "GET":
		a.mu.Lock()
		config, configured := a.config, a.store != nil
		a.mu.Unlock()
		archiveJSON(w, 200, map[string]any{"configured": configured, "config": config})
		return
	case r.URL.Path == "/archive/config" && r.Method == "POST":
		var config ArchiveConfig
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&config) != nil || a.configure(config) != nil {
			http.Error(w, "archive configuration rejected", 422)
			return
		}
		w.WriteHeader(204)
		return
	}
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil && r.URL.Path == "/archive/camera-cleanup" && r.Method == "POST" {
		var input CameraCleanupRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil || input.Tenant == "" || input.Camera == "" {
			http.Error(w, "tenant and camera required", 422)
			return
		}
		archiveJSON(w, 200, CameraCleanupPage{Done: true})
		return
	}
	if store == nil && !(r.URL.Path == "/captures" && r.Method == "DELETE") {
		http.Error(w, "archive not configured", 409)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/archive/objects/") && strings.HasSuffix(r.URL.Path, "/compatible") {
		a.compatibleHTTP(w, r, store)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/archive/exports") {
		a.exportHTTP(w, r, store)
		return
	}
	switch {
	case r.URL.Path == "/archive/camera-cleanup" && r.Method == "POST":
		a.cameraCleanupHTTP(w, r, store)
	case r.URL.Path == "/archive/reduction-preview" && r.Method == "POST":
		var input struct {
			Tenant string
			Target int64
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			http.Error(w, "invalid preview", 422)
			return
		}
		result, err := store.ReductionPreview(input.Tenant, input.Target)
		if err != nil {
			http.Error(w, "invalid reduction", 409)
			return
		}
		archiveJSON(w, 200, result)
	case r.URL.Path == "/archive/reductions" && r.Method == "POST":
		var input struct {
			Tenant  string
			Target  int64
			Expires time.Time
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || time.Until(input.Expires) > 1500*time.Millisecond {
			http.Error(w, "invalid reduction", 422)
			return
		}
		result, err := store.ReduceRecording(r.Context(), input.Tenant, input.Target, input.Expires)
		if err != nil {
			http.Error(w, "reduction unavailable", 409)
			return
		}
		archiveJSON(w, 200, result)
	case r.URL.Path == "/captures" && r.Method == "DELETE":
		tenant := r.URL.Query().Get("tenant")
		if tenant == "" {
			http.Error(w, "tenant required", 422)
			return
		}
		wait := []chan struct{}{}
		a.mu.Lock()
		for _, c := range a.captures {
			if c.plan.Tenant == tenant && (r.URL.Query().Get("camera") == "" || c.plan.Camera == r.URL.Query().Get("camera")) {
				c.cancel()
				wait = append(wait, c.done)
			}
		}
		a.mu.Unlock()
		for _, done := range wait {
			select {
			case <-done:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(204)
	case r.URL.Path == "/captures" && r.Method == "POST":
		var plan CapturePlan
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&plan) != nil || a.apply(plan) != nil {
			http.Error(w, "capture plan rejected", 422)
			return
		}
		w.WriteHeader(204)
	case r.URL.Path == "/captures" && r.Method == "GET":
		a.mu.Lock()
		statuses := []CaptureStatus{}
		for _, c := range a.captures {
			statuses = append(statuses, c.status)
		}
		a.mu.Unlock()
		archiveJSON(w, 200, statuses)
	case strings.HasPrefix(r.URL.Path, "/captures/") && r.Method == "DELETE":
		a.mu.Lock()
		if c := a.captures[strings.TrimPrefix(r.URL.Path, "/captures/")]; c != nil {
			c.cancel()
		}
		a.mu.Unlock()
		w.WriteHeader(204)
	case r.URL.Path == "/archive/limits" && r.Method == "PUT":
		var in struct {
			Tenant string
			Kind   storage.Kind
			Bytes  int64
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || store.SetLimit(in.Tenant, in.Kind, in.Bytes) != nil {
			http.Error(w, "quota cannot be applied", 409)
			return
		}
		w.WriteHeader(204)
	case r.URL.Path == "/archive/usage" && r.Method == "GET":
		archiveJSON(w, 200, map[string]int64{"recording": store.Usage(r.URL.Query().Get("tenant"), storage.Recording), "export": store.Usage(r.URL.Query().Get("tenant"), storage.Export), "allocationUnit": store.AllocationUnit()})
	case r.URL.Path == "/archive/catalog" && r.Method == "GET":
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor", 422)
			return
		}
		items, err := store.CatalogPage(after, 100)
		if err != nil {
			http.Error(w, "catalogue unavailable", 503)
			return
		}
		archiveJSON(w, 200, items)
	case r.URL.Path == "/archive/catalog/ack" && r.Method == "POST":
		var in struct{ Sequence int64 }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || store.Acknowledge(in.Sequence) != nil {
			http.Error(w, "invalid acknowledgement", 422)
			return
		}
		w.WriteHeader(204)
	case strings.HasPrefix(r.URL.Path, "/archive/objects/"):
		id := strings.TrimPrefix(r.URL.Path, "/archive/objects/")
		tenant := r.URL.Query().Get("tenant")
		entry, err := store.Entry(tenant, id)
		if err != nil {
			if !errors.Is(err, storage.ErrNotFound) {
				http.Error(w, "storage unavailable", 503)
				return
			}
			if r.Method == "DELETE" {
				w.WriteHeader(204)
				return
			}
			http.NotFound(w, r)
			return
		}
		if r.Method == "DELETE" {
			if store.Delete(tenant, id, false) != nil {
				http.Error(w, "object protected or unavailable", 409)
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method", 405)
			return
		}
		reader, err := store.Read(tenant, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer reader.Close()
		// Protections expire even if a client stalls. Capture can preempt sooner.
		protection := 10 * time.Second
		if entry.Kind == storage.Export {
			protection = 30 * time.Minute
		}
		timer := time.AfterFunc(protection, func() { reader.Close() })
		defer timer.Stop()
		stopRead := context.AfterFunc(r.Context(), func() { reader.Close() })
		defer stopRead()
		controller := http.NewResponseController(w)
		controller.SetWriteDeadline(time.Now().Add(protection))
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, id+".mp4", entry.Created, reader)
		if err := controller.Flush(); err != nil {
			panic(http.ErrAbortHandler)
		}
	default:
		http.NotFound(w, r)
	}
}

// CopyBounded is shared by streaming export/download paths: a failed read never
// becomes a successful short artifact.
func CopyBounded(dst io.Writer, src io.Reader, n int64) error {
	_, err := io.CopyN(dst, src, n)
	return err
}
