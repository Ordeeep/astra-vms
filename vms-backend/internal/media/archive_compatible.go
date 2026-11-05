package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"vms.local/backend/internal/storage"
)

type BrowserPart struct {
	Init, Data []byte
	Codec      string
	Frames     int
	Duration   time.Duration
}

// TranscodeMP4 converts one independently decodable archive part in memory.
// Input and output are bounded, with no temporary cleartext copy on disk.
func TranscodeMP4(parent context.Context, ffmpeg string, input []byte) (BrowserPart, error) {
	if len(input) == 0 || len(input) > 17<<20 {
		return BrowserPart{}, errors.New("invalid compatible input size")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-nostdin", "-threads", "2", "-protocol_whitelist", "pipe", "-f", "mp4", "-i", "pipe:0", "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", "setpts=PTS-STARTPTS,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-profile:v", "baseline", "-crf", "23", "-bf", "0", "-g", "600", "-keyint_min", "600", "-sc_threshold", "0", "-fps_mode", "passthrough", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedBuffer{limit: 32 << 20}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	p, err := StartOwned(cmd)
	if err != nil {
		return BrowserPart{}, errors.New("compatible encoder unavailable")
	}
	defer p.Stop()
	if err = cmd.Wait(); err != nil {
		return BrowserPart{}, errors.New("compatible encoding failed")
	}
	var result BrowserPart
	raw := out.Bytes()
	fragment := false
	for len(raw) > 0 {
		if len(raw) < 8 {
			return BrowserPart{}, errors.New("invalid converted MP4")
		}
		n := int64(binary.BigEndian.Uint32(raw[:4]))
		if n < 8 || n > int64(len(raw)) {
			return BrowserPart{}, errors.New("invalid converted box")
		}
		typ := string(raw[4:8])
		box := raw[:n]
		switch typ {
		case "ftyp", "moov":
			if fragment {
				return BrowserPart{}, errors.New("invalid converted initialization")
			}
			result.Init = append(result.Init, box...)
		case "moof", "mdat":
			fragment = true
			result.Data = append(result.Data, box...)
		case "mfra", "free":
		default:
			return BrowserPart{}, errors.New("unexpected converted box")
		}
		raw = raw[n:]
	}
	var init fmp4.Init
	if init.Unmarshal(bytes.NewReader(result.Init)) != nil || len(init.Tracks) != 1 {
		return BrowserPart{}, errors.New("invalid converted video")
	}
	codec, ok := init.Tracks[0].Codec.(*codecs.H264)
	if !ok || len(codec.SPS) < 4 {
		return BrowserPart{}, errors.New("converted codec is not H264")
	}
	result.Codec = "avc1." + hex.EncodeToString(codec.SPS[1:4])
	var parts fmp4.Parts
	if parts.Unmarshal(result.Data) != nil || len(parts) == 0 {
		return BrowserPart{}, errors.New("missing converted video")
	}
	for _, p := range parts {
		if len(p.Tracks) != 1 {
			return BrowserPart{}, errors.New("invalid converted tracks")
		}
		for _, sample := range p.Tracks[0].Samples {
			result.Frames++
			result.Duration += time.Duration(sample.Duration) * time.Second / time.Duration(init.Tracks[0].TimeScale)
		}
	}
	return result, nil
}

type cachedBrowserPart struct {
	part BrowserPart
	used time.Time
}
type compatibleFlight struct {
	done chan struct{}
	part BrowserPart
	err  error
}
type compatibilityCache struct {
	mu      sync.Mutex
	items   map[string]cachedBrowserPart
	flights map[string]*compatibleFlight
	bytes   int
	slots   chan struct{}
}

func newCompatibilityCache() *compatibilityCache {
	return &compatibilityCache{items: map[string]cachedBrowserPart{}, flights: map[string]*compatibleFlight{}, slots: make(chan struct{}, 4)}
}
func (c *compatibilityCache) get(ctx context.Context, key string, load func() (BrowserPart, error)) (BrowserPart, error) {
	c.mu.Lock()
	if v, ok := c.items[key]; ok {
		v.used = time.Now()
		c.items[key] = v
		c.mu.Unlock()
		return v.part, nil
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return BrowserPart{}, ctx.Err()
		case <-f.done:
			return f.part, f.err
		}
	}
	// Admission is immediate: an unbounded queue would retain caller data.
	select {
	case c.slots <- struct{}{}:
	default:
		c.mu.Unlock()
		return BrowserPart{}, errors.New("compatible conversion capacity exhausted")
	}
	f := &compatibleFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	part, err := load()
	c.mu.Lock()
	defer c.mu.Unlock()
	<-c.slots
	f.part, f.err = part, err
	delete(c.flights, key)
	if err == nil {
		size := len(part.Init) + len(part.Data)
		for len(c.items) > 0 && (c.bytes+size > 64<<20 || len(c.items) >= 16) {
			old := ""
			var at time.Time
			for k, v := range c.items {
				if old == "" || v.used.Before(at) {
					old, at = k, v.used
				}
			}
			v := c.items[old]
			c.bytes -= len(v.part.Init) + len(v.part.Data)
			delete(c.items, old)
		}
		if size <= 64<<20 {
			c.items[key] = cachedBrowserPart{part: part, used: time.Now()}
			c.bytes += size
		}
	}
	close(f.done)
	return part, err
}
func (a *ArchiveService) compatibleHTTP(w http.ResponseWriter, r *http.Request, store *storage.Store) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method", 405)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/archive/objects/"), "/compatible")
	tenant := r.URL.Query().Get("tenant")
	number, err := strconv.Atoi(r.URL.Query().Get("part"))
	kind := r.URL.Query().Get("kind")
	if tenant == "" || id == "" || strings.Contains(id, "/") || err != nil || number < 0 || (kind != "init" && kind != "data") {
		http.Error(w, "invalid compatible part", 422)
		return
	}
	// Check current tenant ownership on cache hits as well as conversion misses.
	entry, err := store.Entry(tenant, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "storage unavailable", 503)
		}
		return
	}
	var index SegmentIndex
	if entry.Kind != storage.Recording || json.Unmarshal(entry.Metadata, &index) != nil || index.Tenant != tenant || number >= len(index.Parts) || index.InitLength < 1 || index.InitLength > 64<<10 {
		http.Error(w, "invalid archive index", 422)
		return
	}
	part := index.Parts[number]
	if part.Offset < index.InitLength || part.Length < 1 || part.Length > 16<<20 || part.Offset > entry.Size || part.Length > entry.Size-part.Offset || part.Frames < 1 || part.Frames > 300 || part.Timescale != 90000 || len(part.Durations) != part.Frames || !part.End.After(part.Start) || part.End.Sub(part.Start) > 12*time.Second || part.Width < 1 || part.Height < 1 || part.Width > 8192 || part.Height > 4320 {
		http.Error(w, "invalid archive part", 422)
		return
	}
	digest := sha256.Sum256(entry.Metadata)
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%x", tenant, id, number, digest)
	// A single request's cancellation terminates its worker and releases admission.
	converted, err := a.compatible.get(r.Context(), key, func() (BrowserPart, error) {
		convertCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stopService := context.AfterFunc(a.ctx, cancel)
		defer stopService()
		reader, e := store.Read(tenant, id)
		if e != nil {
			return BrowserPart{}, errors.New("archive part unavailable")
		}
		defer reader.Close()
		deadline := time.AfterFunc(10*time.Second, func() { reader.Close() })
		defer deadline.Stop()
		stop := context.AfterFunc(r.Context(), func() { reader.Close() })
		defer stop()
		input := make([]byte, index.InitLength+part.Length)
		if _, e = io.ReadFull(reader, input[:index.InitLength]); e != nil {
			return BrowserPart{}, errors.New("archive initialization unavailable")
		}
		if _, e = reader.Seek(part.Offset, io.SeekStart); e != nil {
			return BrowserPart{}, errors.New("archive part unavailable")
		}
		if _, e = io.ReadFull(reader, input[index.InitLength:]); e != nil {
			return BrowserPart{}, errors.New("archive part unavailable")
		}
		reader.Close() // release recording protection before CPU conversion
		converted, e := TranscodeMP4(convertCtx, "ffmpeg", input)
		if e != nil {
			return BrowserPart{}, e
		}
		delta := converted.Duration - part.End.Sub(part.Start)
		if converted.Frames != part.Frames || delta > 5*time.Millisecond || delta < -5*time.Millisecond {
			return BrowserPart{}, errors.New("converted frame clock mismatch")
		}
		return converted, nil
	})
	if err != nil {
		http.Error(w, "compatible playback unavailable", 503)
		return
	}
	// Deletion/preemption during conversion must not expose a stale cache entry.
	if _, err = store.Entry(tenant, id); err != nil {
		http.NotFound(w, r)
		return
	}
	body := converted.Data
	if kind == "init" {
		body = converted.Init
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Video-Codec", converted.Codec)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, id+"-"+strconv.Itoa(number)+"-"+kind+".mp4", entry.Created, bytes.NewReader(body))
}
