package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vms.local/backend/internal/media"
)

type archiveSegment struct {
	ID      string             `json:"id"`
	Camera  string             `json:"cameraId"`
	Site    string             `json:"siteId"`
	Source  string             `json:"sourceVersion"`
	Profile string             `json:"profile"`
	Start   time.Time          `json:"start"`
	End     time.Time          `json:"end"`
	Bytes   int64              `json:"bytes"`
	Index   media.SegmentIndex `json:"-"`
}

const segmentSelect = "SELECT id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,metadata FROM archive_segments"

func (a *App) recordingCameras(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT c.id,c.name,c.site_id,ARRAY(SELECT DISTINCT s.site_id FROM archive_segments s WHERE s.tenant_id=c.tenant_id AND s.camera_id=c.id AND s.available) FROM cameras c WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND c.id>$2 ORDER BY c.id LIMIT 201`, tenant, r.URL.Query().Get("cursor"))
	if err != nil {
		dbFailure(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	cursor := ""
	count := 0
	for rows.Next() {
		var camera, name, current string
		var origins []string
		if rows.Scan(&camera, &name, &current, &origins) != nil {
			dbFailure(w)
			return
		}
		count++
		if count > 200 {
			break
		}
		cursor = camera
		allowed, export := false, false
		for _, site := range append(origins, current) {
			allowed = allowed || permits(grants, tenant, "playback", site, camera)
			export = export || (permits(grants, tenant, "playback", site, camera) && permits(grants, tenant, "export", site, camera))
		}
		if allowed {
			items = append(items, map[string]any{"id": camera, "name": name, "capabilities": map[string]bool{"playback": true, "export": export}})
		}
	}
	if rows.Err() != nil {
		dbFailure(w)
		return
	}
	if count <= 200 {
		cursor = ""
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}

func (a *App) segments(ctx context.Context, tenant, camera string, start, end time.Time) ([]archiveSegment, error) {
	rows, err := a.db.Query(ctx, segmentSelect+" WHERE tenant_id=$1 AND camera_id=$2 AND available AND end_at>$3 AND start_at<$4 AND EXISTS(SELECT 1 FROM cameras WHERE cameras.tenant_id=archive_segments.tenant_id AND cameras.id=archive_segments.camera_id AND cameras.deleted_at IS NULL) ORDER BY start_at,id LIMIT 10001", tenant, camera, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []archiveSegment{}
	for rows.Next() {
		var s archiveSegment
		var raw []byte
		if err = rows.Scan(&s.ID, &s.Camera, &s.Site, &s.Source, &s.Profile, &s.Start, &s.End, &s.Bytes, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &s.Index) != nil {
			return nil, errors.New("invalid archive index")
		}
		out = append(out, s)
	}
	if len(out) > 10000 {
		return nil, errors.New("archive window too large")
	}
	return out, rows.Err()
}
func timeWindow(r *http.Request, max time.Duration) (time.Time, time.Time, error) {
	start, e1 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("start"))
	end, e2 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("end"))
	if e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > max {
		return start, end, errors.New("invalid time window")
	}
	return start, end, nil
}
func (a *App) timeline(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	start, end, err := timeWindow(r, 24*time.Hour)
	if err != nil {
		problem(w, 422, "invalid_time_window")
		return
	}
	segments, err := a.segments(r.Context(), tenant, r.PathValue("camera"), start, end)
	if err != nil {
		dbFailure(w)
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	out := []archiveSegment{}
	for _, s := range segments {
		if permits(grants, tenant, "playback", s.Site, s.Camera) {
			out = append(out, s)
		}
	}
	response(w, 200, map[string]any{"items": out, "start": start, "end": end})
}

type playbackSession struct {
	ID, Camera string
	Start, End time.Time
}

func (a *App) loadPlayback(r *http.Request, id identity, tenant string) (playbackSession, error) {
	var s playbackSession
	err := a.db.QueryRow(r.Context(), "SELECT id,camera_id,start_at,end_at FROM playback_sessions WHERE id=$1 AND tenant_id=$2 AND context_id=$3 AND session_hash=$4 AND expires>now()", r.PathValue("playback"), tenant, r.Header.Get("X-VMS-Context"), id.SessionHash).Scan(&s.ID, &s.Camera, &s.Start, &s.End)
	return s, err
}
func (a *App) createPlayback(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Camera             string    `json:"cameraId"`
		Start              time.Time `json:"start"`
		End                time.Time `json:"end"`
		Codecs             []string  `json:"codecs"`
		ForceCompatibility bool      `json:"forceCompatibility"`
	}
	if !body(w, r, &in) {
		return
	}
	if !in.End.After(in.Start) || in.End.Sub(in.Start) > time.Hour || len(in.Codecs) > 16 {
		problem(w, 422, "invalid_playback_window")
		return
	}
	segments, err := a.segments(r.Context(), tenant, in.Camera, in.Start, in.End)
	if err != nil {
		dbFailure(w)
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	found := false
	var first time.Time
	for _, s := range segments {
		if permits(grants, tenant, "playback", s.Site, s.Camera) {
			found = true
			first = s.Start
			break
		}
	}
	if !found {
		problem(w, 404, "recording_unavailable")
		return
	}
	if first.Sub(in.Start) > time.Microsecond {
		response(w, 409, map[string]any{"code": "recording_gap", "nextStart": first})
		return
	}
	sid := newID()
	if _, err = a.db.Exec(r.Context(), "WITH inserted AS (INSERT INTO playback_sessions(id,tenant_id,camera_id,context_id,session_hash,start_at,end_at,expires) VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '10 seconds') RETURNING id) INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) SELECT $8,$2,'playback.started',$3 FROM inserted", sid, tenant, in.Camera, r.Header.Get("X-VMS-Context"), id.SessionHash, in.Start, in.End, id.User.ID); err != nil {
		dbFailure(w)
		return
	}
	mode := deliveryMode(in.Codecs, in.ForceCompatibility)
	delivery := "native"
	if mode != "native" {
		delivery = "compatible"
	}
	response(w, 201, map[string]any{"id": sid, "delivery": delivery, "manifest": "/api/v1/tenants/" + tenant + "/playback-sessions/" + sid + "/manifest.m3u8?delivery=" + mode})
}
func (a *App) playbackControl(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if r.Method == "DELETE" {
		a.db.Exec(r.Context(), "DELETE FROM playback_sessions WHERE id=$1 AND tenant_id=$2 AND context_id=$3 AND session_hash=$4", r.PathValue("playback"), tenant, r.Header.Get("X-VMS-Context"), id.SessionHash)
		w.WriteHeader(204)
		return
	}
	s, err := a.loadPlayback(r, id, tenant)
	if err != nil {
		problem(w, 410, "playback_ended")
		return
	}
	segments, err := a.segments(r.Context(), tenant, s.Camera, s.Start, s.End)
	if err != nil {
		dbFailure(w)
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	any := false
	for _, segment := range segments {
		if permits(grants, tenant, "playback", segment.Site, segment.Camera) {
			any = true
			break
		}
	}
	if !any {
		problem(w, 403, "forbidden")
		return
	}
	a.db.Exec(r.Context(), "UPDATE playback_sessions SET expires=now()+interval '10 seconds' WHERE id=$1", s.ID)
	w.WriteHeader(204)
}
func (a *App) playbackManifest(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	session, err := a.loadPlayback(r, id, tenant)
	if err != nil {
		problem(w, 410, "playback_ended")
		return
	}
	segments, err := a.segments(r.Context(), tenant, session.Camera, session.Start, session.End)
	if err != nil {
		dbFailure(w)
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	type selected struct {
		s archiveSegment
		p media.PartIndex
		n int
	}
	parts := []selected{}
	target := 1
	for _, s := range segments {
		if !permits(grants, tenant, "playback", s.Site, s.Camera) {
			continue
		}
		for n, p := range s.Index.Parts {
			if p.End.After(session.Start) && p.Start.Before(session.End) {
				parts = append(parts, selected{s, p, n})
				target = max(target, int(math.Ceil(p.End.Sub(p.Start).Seconds())))
			}
		}
	}
	if len(parts) == 0 {
		problem(w, 404, "recording_unavailable")
		return
	}
	if parts[0].p.Start.Sub(session.Start) > time.Microsecond {
		response(w, 409, map[string]any{"code": "recording_gap", "nextStart": parts[0].p.Start})
		return
	}
	var nextStart *time.Time
	for i := 1; i < len(parts); i++ {
		if parts[i].p.Start.Sub(parts[i-1].p.End) > time.Microsecond {
			at := parts[i].p.Start
			nextStart = &at
			parts = parts[:i]
			break
		}
	}
	var b strings.Builder
	mode := r.URL.Query().Get("delivery")
	if mode != "native" && mode != "converted" {
		mode = "compatible"
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n", target)
	offset := max(0, session.Start.Sub(parts[0].p.Start).Seconds())
	fmt.Fprintf(&b, "#EXT-X-START:TIME-OFFSET=%.6f,PRECISE=YES\n", offset)
	base := "/api/v1/tenants/" + tenant + "/playback-sessions/" + session.ID
	mapping := []map[string]any{}
	var elapsed float64
	for i, p := range parts {
		if i > 0 {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		initURL := fmt.Sprintf("%s/segments/%s/init", base, p.s.ID)
		partURL := fmt.Sprintf("%s/segments/%s/parts/%d", base, p.s.ID, p.n)
		if archiveNeedsConversion(p.p.Codec, mode) {
			initURL += fmt.Sprintf("?compatible=1&part=%d", p.n)
			partURL += "?compatible=1"
		}
		fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%s\"\n#EXT-X-PROGRAM-DATE-TIME:%s\n#EXTINF:%.6f,\n%s\n", initURL, p.p.Start.Format(time.RFC3339Nano), p.p.End.Sub(p.p.Start).Seconds(), partURL)
		mapping = append(mapping, map[string]any{"mediaStart": elapsed, "start": p.p.Start, "end": p.p.End})
		elapsed += p.p.End.Sub(p.p.Start).Seconds()
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	if r.URL.Query().Get("format") == "json" {
		response(w, 200, map[string]any{"mapping": mapping, "offset": offset, "nextStart": nextStart})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	io.WriteString(w, b.String())
}
func (a *App) playbackBytes(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	session, err := a.loadPlayback(r, id, tenant)
	if err != nil {
		problem(w, 410, "playback_ended")
		return
	}
	var s archiveSegment
	var raw []byte
	err = a.db.QueryRow(r.Context(), segmentSelect+" WHERE tenant_id=$1 AND camera_id=$2 AND id=$3 AND available", tenant, session.Camera, r.PathValue("segment")).Scan(&s.ID, &s.Camera, &s.Site, &s.Source, &s.Profile, &s.Start, &s.End, &s.Bytes, &raw)
	if err != nil || json.Unmarshal(raw, &s.Index) != nil || !s.End.After(session.Start) || !s.Start.Before(session.End) {
		problem(w, 404, "recording_unavailable")
		return
	}
	authorized := func(ctx context.Context) bool {
		var active bool
		if a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM playback_sessions WHERE id=$1 AND expires>now())", session.ID).Scan(&active) != nil || !active {
			return false
		}
		return a.sessionStillActive(ctx, id) && contextValid(ctx, a.db, r, id) && a.allowed(ctx, id, tenant, "playback", s.Site, s.Camera)
	}
	if !authorized(r.Context()) {
		problem(w, 403, "forbidden")
		return
	}
	offset, length := int64(0), s.Index.InitLength
	compatible := r.URL.Query().Get("compatible") == "1"
	part := r.PathValue("part")
	kind := "data"
	if part == "" {
		kind = "init"
		if compatible {
			part = r.URL.Query().Get("part")
		}
	}
	partNumber := -1
	if part != "" {
		n, e := strconv.Atoi(part)
		if e != nil || n < 0 || n >= len(s.Index.Parts) {
			problem(w, 404, "part_unavailable")
			return
		}
		p := s.Index.Parts[n]
		if !p.End.After(session.Start) || !p.Start.Before(session.End) {
			problem(w, 403, "outside_playback_window")
			return
		}
		offset, length = p.Offset, p.Length
		partNumber = n
	}
	if compatible {
		if partNumber < 0 {
			problem(w, 404, "part_unavailable")
			return
		}
		a.proxyCompatibleArchive(w, r, tenant, s.ID, partNumber, kind, authorized)
		return
	}
	a.proxyArchiveBytes(w, r, tenant, s.ID, offset, length, authorized)
}
func (a *App) sessionStillActive(ctx context.Context, id identity) bool {
	var ok bool
	err := a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sessions s JOIN operators o ON o.id=s.operator_id WHERE s.token_hash=$1 AND s.expires>now() AND o.active)", id.SessionHash).Scan(&ok)
	return err == nil && ok
}
func (a *App) proxyArchiveBytes(w http.ResponseWriter, r *http.Request, tenant, object string, offset, length int64, authorized func(context.Context) bool) {
	if length <= 0 {
		problem(w, 404, "empty_media")
		return
	}
	begin, end := int64(0), length-1
	status := 200
	if header := r.Header.Get("Range"); header != "" {
		if !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
			w.WriteHeader(416)
			return
		}
		parts := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
		if len(parts) != 2 {
			w.WriteHeader(416)
			return
		}
		var err error
		if parts[0] == "" {
			var suffix int64
			suffix, err = strconv.ParseInt(parts[1], 10, 64)
			if suffix <= 0 {
				err = errors.New("range")
			}
			begin = max(0, length-suffix)
		} else {
			begin, err = strconv.ParseInt(parts[0], 10, 64)
		}
		if parts[0] != "" && parts[1] != "" {
			var endErr error
			end, endErr = strconv.ParseInt(parts[1], 10, 64)
			if endErr != nil {
				err = endErr
			}
			end = min(end, length-1)
		}
		if err != nil || begin < 0 || end < begin || begin >= length {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", length))
			w.WriteHeader(416)
			return
		}
		status = 206
	}
	maxTransfer := 10 * time.Second
	if strings.Contains(r.URL.Path, "/exports/") {
		maxTransfer = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxTransfer)
	defer cancel()
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, done := context.WithTimeout(ctx, 400*time.Millisecond)
				ok := authorized(check)
				done()
				if !ok {
					cancel()
					return
				}
			}
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", a.opts.WorkerURL+"/archive/objects/"+url.PathEscape(object)+"?tenant="+url.QueryEscape(tenant), nil)
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.opts.WorkerSecret)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset+begin, offset+end))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		a.db.Exec(r.Context(), "UPDATE archive_segments SET available=false,discard_reason=coalesce(discard_reason,'storage_reclaimed') WHERE tenant_id=$1 AND id=$2", tenant, object)
		problem(w, 410, "recording_unavailable")
		return
	}
	if res.StatusCode != 206 || res.ContentLength != end-begin+1 {
		problem(w, 502, "invalid_media_range")
		return
	}
	if !authorized(ctx) {
		problem(w, 403, "forbidden")
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "video/mp4")
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-begin+1, 10))
	if status == 206 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", begin, end, length))
	}
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(time.Now().Add(750*time.Millisecond)) != nil {
		problem(w, 503, "bounded_delivery_unavailable")
		return
	}
	interrupted := make(chan struct{})
	stopWrite := context.AfterFunc(ctx, func() { controller.SetWriteDeadline(time.Now()); close(interrupted) })
	defer func() {
		if !stopWrite() {
			<-interrupted
		}
		controller.SetWriteDeadline(time.Time{})
	}()
	w.WriteHeader(status)
	remaining := end - begin + 1
	buf := make([]byte, 32<<10)
	for remaining > 0 {
		n, e := io.ReadFull(res.Body, buf[:min(int64(len(buf)), remaining)])
		if e != nil || ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		controller.SetWriteDeadline(time.Now().Add(750 * time.Millisecond))
		written, e := w.Write(buf[:n])
		if e != nil || written != n {
			panic(http.ErrAbortHandler)
		}
		remaining -= int64(n)
	}
	if err := controller.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
func (a *App) registerPlayback(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/recording-cameras", a.protected(a.recordingCameras))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/cameras/{camera}/timeline", a.protected(a.timeline))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/playback-sessions", a.protected(a.createPlayback))
	for _, m := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(m+" /api/v1/tenants/{tenant}/playback-sessions/{playback}", a.protected(a.playbackControl))
	}
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/playback-sessions/{playback}/manifest.m3u8", a.protected(a.playbackManifest))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/playback-sessions/{playback}/segments/{segment}/init", a.protected(a.playbackBytes))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/playback-sessions/{playback}/segments/{segment}/parts/{part}", a.protected(a.playbackBytes))
}
