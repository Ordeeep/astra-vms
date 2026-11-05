package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"vms.local/backend/internal/storage"
)

type ExportRequest struct {
	Plan            ExportPlan `json:"plan"`
	Expires         time.Time  `json:"expires"`
	ArtifactExpires time.Time  `json:"artifactExpires"`
}
type ExportStatus struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Code        string    `json:"code,omitempty"`
	Frames      int64     `json:"frames"`
	Total       int64     `json:"total"`
	Content     string    `json:"content,omitempty"`
	Report      string    `json:"report,omitempty"`
	Bytes       int64     `json:"bytes"`
	ReportBytes int64     `json:"reportBytes"`
	SHA256      string    `json:"sha256,omitempty"`
	Expires     time.Time `json:"expires"`
}
type exportTask struct {
	hash     string
	tenant   string
	expires  time.Time
	cancel   context.CancelFunc
	status   ExportStatus
	done     chan struct{}
	finished time.Time
}
type exportArtifact struct {
	Job    string       `json:"job"`
	Role   string       `json:"role"`
	Result ExportStatus `json:"result"`
}

func IndexHash(index SegmentIndex) string {
	raw, _ := json.Marshal(index)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func exportHash(plan ExportPlan) string {
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a *ArchiveService) exportHTTP(w http.ResponseWriter, r *http.Request, store *storage.Store) {
	if r.URL.Path == "/archive/exports" && r.Method == "DELETE" {
		tenant := r.URL.Query().Get("tenant")
		if tenant == "" {
			http.Error(w, "tenant required", 422)
			return
		}
		a.mu.Lock()
		wait := []chan struct{}{}
		for _, job := range a.exports {
			if job.tenant == tenant && job.status.Status == "running" {
				job.cancel()
				wait = append(wait, job.done)
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
		return
	}
	if r.URL.Path == "/archive/exports" && r.Method == "POST" {
		var request ExportRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&request) != nil || request.Plan.ValidateEnvelope() != nil || request.Plan.ID == "" || request.Plan.Tenant == "" || time.Until(request.Expires) <= 0 || time.Until(request.Expires) > 1500*time.Millisecond || time.Until(request.ArtifactExpires) <= 0 || time.Until(request.ArtifactExpires) > 721*time.Hour {
			http.Error(w, "invalid export request", 422)
			return
		}
		a.mu.Lock()
		if a.ctx.Err() != nil {
			a.mu.Unlock()
			http.Error(w, "worker stopping", 503)
			return
		}
		if task := a.exports[request.Plan.ID]; task != nil {
			if task.hash != exportHash(request.Plan) || task.tenant != request.Plan.Tenant {
				a.mu.Unlock()
				http.Error(w, "export identity conflict", 409)
				return
			}
			if task.status.Status == "running" && time.Now().Before(task.expires) {
				task.expires = request.Expires
			}
			status := task.status
			a.mu.Unlock()
			archiveJSON(w, 200, status)
			return
		}
		for _, job := range a.exports {
			if job.status.Status == "running" {
				a.mu.Unlock()
				http.Error(w, "export worker busy", 409)
				return
			}
		}
		if len(a.exports) >= 1000 {
			a.mu.Unlock()
			http.Error(w, "export status capacity", 429)
			return
		}
		ctx, cancel := context.WithCancel(a.ctx)
		job := &exportTask{hash: exportHash(request.Plan), tenant: request.Plan.Tenant, expires: request.Expires, cancel: cancel, done: make(chan struct{}), status: ExportStatus{ID: request.Plan.ID, Status: "running", Total: request.Plan.Frames(), Expires: request.ArtifactExpires}}
		a.exports[request.Plan.ID] = job
		a.wg.Add(1)
		go a.renderExport(ctx, request, job, store)
		status := job.status
		a.mu.Unlock()
		archiveJSON(w, 202, status)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/archive/exports/")
	tenant := r.URL.Query().Get("tenant")
	a.mu.Lock()
	job := a.exports[id]
	if job != nil && job.tenant == tenant {
		if r.Method == "PATCH" {
			var in struct {
				Expires time.Time `json:"expires"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || job.status.Status != "running" || !time.Now().Before(job.expires) || time.Until(in.Expires) <= 0 || time.Until(in.Expires) > 1500*time.Millisecond {
				a.mu.Unlock()
				http.Error(w, "export lease rejected", 409)
				return
			}
			job.expires = in.Expires
			a.mu.Unlock()
			w.WriteHeader(204)
			return
		}
		if r.Method == "DELETE" {
			job.cancel()
			done := job.done
			a.mu.Unlock()
			select {
			case <-done:
			case <-r.Context().Done():
				return
			}
			if !deleteExportObjects(w, store, tenant, id) {
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" {
			status := job.status
			a.mu.Unlock()
			archiveJSON(w, 200, status)
			return
		}
	}
	a.mu.Unlock()
	if r.Method == "DELETE" {
		if !deleteExportObjects(w, store, tenant, id) {
			return
		}
		w.WriteHeader(204)
		return
	}
	// Completed artifacts survive worker restarts. In-flight jobs do not.
	if r.Method == "GET" {
		cursor := ""
		for r.Context().Err() == nil {
			items, next, err := store.ObjectsPage(tenant, storage.Export, cursor, 100)
			if err != nil {
				http.Error(w, "storage unavailable", 503)
				return
			}
			for _, entry := range items {
				var meta exportArtifact
				if entry.Finalized && json.Unmarshal(entry.Metadata, &meta) == nil && meta.Job == id && meta.Role == "report" {
					archiveJSON(w, 200, meta.Result)
					return
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	http.NotFound(w, r)
}
func deleteExportObjects(w http.ResponseWriter, store *storage.Store, tenant, id string) bool {
	cursor := ""
	for {
		items, next, err := store.ObjectsPage(tenant, storage.Export, cursor, 100)
		if err != nil {
			http.Error(w, "storage unavailable", 503)
			return false
		}
		for _, entry := range items {
			var meta exportArtifact
			if json.Unmarshal(entry.Metadata, &meta) == nil && meta.Job == id {
				if err := store.Delete(tenant, entry.ID, false); err != nil && !errors.Is(err, storage.ErrNotFound) {
					http.Error(w, "artifact protected or cleanup pending", 409)
					return false
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return true
}

type exportWriter struct {
	writer   *storage.Writer
	size     int64
	ctx      context.Context
	deadline func() time.Time
}

func (w *exportWriter) Write(p []byte) (int, error) {
	if err := w.writer.GrowUntil(w.ctx, w.size+int64(len(p)), false, w.deadline()); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	w.size += int64(n)
	return n, err
}
func (a *ArchiveService) renderExport(ctx context.Context, request ExportRequest, job *exportTask, store *storage.Store) {
	defer a.wg.Done()
	defer func() { a.mu.Lock(); job.finished = time.Now(); a.mu.Unlock(); close(job.done) }()
	defer job.cancel()
	var content, report *storage.Writer
	success := false
	code := "export_failed"
	defer func() {
		if !success {
			for _, writer := range []*storage.Writer{content, report} {
				if writer != nil {
					writer.Abort()
					store.Delete(request.Plan.Tenant, writer.ID(), false)
				}
			}
			a.mu.Lock()
			job.status.Status = "failed"
			job.status.Code = code
			a.mu.Unlock()
		}
	}()
	deadline := func() time.Time { a.mu.Lock(); defer a.mu.Unlock(); return job.expires }
	for n, clip := range request.Plan.Clips {
		entry, e := store.Entry(request.Plan.Tenant, clip.ID)
		var index SegmentIndex
		if e != nil || !entry.Finalized || entry.Kind != storage.Recording || json.Unmarshal(entry.Metadata, &index) != nil || IndexHash(index) != clip.MetadataHash || index.Tenant != request.Plan.Tenant || index.Camera != request.Plan.Camera {
			code = "source_changed_repreview"
			return
		}
		request.Plan.Clips[n].Index = index
	}
	var err error
	content, err = store.CreateUntil(ctx, request.Plan.Tenant, storage.Export, 64<<10, deadline())
	if err != nil {
		code = "export_budget_unavailable"
		return
	}
	tag, _ := json.Marshal(exportArtifact{Job: request.Plan.ID, Role: "partial", Result: job.status})
	if content.Tag(tag) != nil {
		return
	}
	output := &exportWriter{writer: content, ctx: ctx, deadline: deadline}
	hash := sha256.New()
	err = RenderExport(ctx, "ffmpeg", request.Plan, func(ctx context.Context, clip ExportClip) (io.ReadCloser, error) {
		reader, err := store.Read(request.Plan.Tenant, clip.ID)
		if err != nil {
			return nil, errors.New("source unavailable")
		}
		return CanonicalExportInput(ctx, reader, clip.Index), nil
	}, io.MultiWriter(output, hash), func(frames int64) { a.mu.Lock(); job.status.Frames = frames; a.mu.Unlock() })
	if err != nil {
		code = "source_or_encoding_failed"
		if ctx.Err() != nil {
			code = "export_cancelled"
		}
		if errors.Is(err, storage.ErrCapacity) {
			code = "export_budget_unavailable"
		}
		return
	}
	if ctx.Err() != nil || !time.Now().Before(deadline()) {
		code = "export_authority_expired"
		return
	}
	a.mu.Lock()
	result := job.status
	a.mu.Unlock()
	result.Status = "completed"
	result.Content = content.ID()
	result.Bytes = output.size
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	origins := []map[string]any{}
	for _, clip := range request.Plan.Clips {
		includedStart, includedEnd := clip.Index.Start, clip.Index.End
		if request.Plan.Start.After(includedStart) {
			includedStart = request.Plan.Start
		}
		if request.Plan.End.Before(includedEnd) {
			includedEnd = request.Plan.End
		}
		origins = append(origins, map[string]any{"object": clip.ID, "camera": clip.Index.Camera, "site": clip.Index.Site, "source": clip.Index.Source, "profile": clip.Index.Profile, "sourceStart": clip.Index.Start, "sourceEnd": clip.Index.End, "includedStart": includedStart, "includedEnd": includedEnd, "metadataHash": clip.MetadataHash})
	}
	document := map[string]any{"schema": "vms-export/1", "job": request.Plan.ID, "tenant": request.Plan.Tenant, "camera": request.Plan.Camera, "selectedStart": request.Plan.Start, "selectedEnd": request.Plan.End, "durationSeconds": float64(request.Plan.Frames()) / float64(request.Plan.FPS), "frames": request.Plan.Frames(), "fps": request.Plan.FPS, "width": request.Plan.Width, "height": request.Plan.Height, "normalization": "H.264 CFR; frame active at each output instant; proportional scale and black padding; no audio or overlay", "timeBasis": "server UTC correlated with source RTP", "gaps": request.Plan.Gaps, "gapFill": "generated black frames, not camera images", "origins": origins, "mp4Bytes": result.Bytes, "mp4Sha256": result.SHA256, "expires": request.ArtifactExpires}
	document["requestor"] = request.Plan.Requestor
	document["createdAt"] = request.Plan.CreatedAt
	document["displayTimezone"] = request.Plan.DisplayTimezone
	raw, _ := json.MarshalIndent(document, "", "  ")
	report, err = store.CreateUntil(ctx, request.Plan.Tenant, storage.Export, int64(len(raw)), deadline())
	if err != nil {
		code = "export_budget_unavailable"
		return
	}
	if report.Tag(tag) != nil {
		return
	}
	if _, err = report.Write(raw); err != nil {
		return
	}
	result.Report = report.ID()
	result.ReportBytes = int64(len(raw))
	metadata, _ := json.Marshal(exportArtifact{Job: result.ID, Role: "content", Result: result})
	if err = content.FinishWithMetadata(metadata); err != nil {
		return
	}
	metadata, _ = json.Marshal(exportArtifact{Job: result.ID, Role: "report", Result: result})
	if err = report.FinishWithMetadata(metadata); err != nil {
		return
	}
	if ctx.Err() != nil || !time.Now().Before(deadline()) {
		code = "export_authority_expired"
		return
	}
	a.mu.Lock()
	job.status = result
	a.mu.Unlock()
	success = true
}
