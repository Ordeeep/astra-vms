package media

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/rtp"
)

// Private worker contract. URLs never appear in public observations.
type AvailabilityProfile struct {
	ID    string   `json:"id"`
	URL   string   `json:"url"`
	Paths []string `json:"paths"`
}
type AvailabilityRequest struct {
	Tenant             string                `json:"tenant"`
	Camera             string                `json:"camera"`
	Source             string                `json:"source"`
	ConnectionRevision int64                 `json:"connectionRevision"`
	Profiles           []AvailabilityProfile `json:"profiles"`
}
type VideoObservation struct {
	Profile     string     `json:"profile"`
	Available   bool       `json:"available"`
	Complete    bool       `json:"complete"`
	Code        string     `json:"code,omitempty"`
	CheckedAt   time.Time  `json:"checkedAt"`
	LastVideoAt *time.Time `json:"lastVideoAt,omitempty"`
}
type AvailabilityResult struct {
	Profiles []VideoObservation `json:"profiles"`
}

// ProbeVideo requires an assembled picture, not SDP or RTP headers.
// It does not decode pixels, transcode, write files, or retain a connection.
func hasVideoPicture(au [][]byte, hevc bool) bool {
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		if hevc {
			if len(nalu) >= 2 && (nalu[0]>>1)&63 <= 31 {
				return true
			}
		} else if nalu[0]&31 >= 1 && nalu[0]&31 <= 5 {
			return true
		}
	}
	return false
}
func ProbeVideo(ctx context.Context, address string) (at time.Time, code string) {
	u, err := base.ParseURL(address)
	if err != nil {
		return at, "invalid_configuration"
	}
	tcp := gortsplib.ProtocolTCP
	c := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &tcp, ReadTimeout: 4 * time.Second, WriteTimeout: 3 * time.Second,
		DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}
	if c.Start() != nil {
		return at, "connection_failed"
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, c.Close)
	defer stop()
	classify := func(err error, fallback string) string {
		var status liberrors.ErrClientBadStatusCode
		if errors.As(err, &status) && (status.Code == base.StatusUnauthorized || status.Code == base.StatusForbidden) {
			return "authentication_failed"
		}
		if ctx.Err() != nil {
			return "video_timeout"
		}
		return fallback
	}
	desc, _, err := c.Describe(u)
	if err != nil {
		return at, classify(err, "description_failed")
	}
	var h264 *format.H264
	var h265 *format.H265
	medi := desc.FindFormat(&h264)
	var selected format.Format
	var decode func(*rtp.Packet) ([][]byte, error)
	if medi != nil {
		decoder, e := h264.CreateDecoder()
		if e != nil {
			return at, "video_unavailable"
		}
		selected = h264
		decode = decoder.Decode
	} else {
		medi = desc.FindFormat(&h265)
		if medi == nil {
			return at, "unsupported_video"
		}
		decoder, e := h265.CreateDecoder()
		if e != nil {
			return at, "video_unavailable"
		}
		selected = h265
		decode = decoder.Decode
	}
	if _, err = c.Setup(desc.BaseURL, medi, 0, 0); err != nil {
		return at, classify(err, "setup_failed")
	}
	frames := make(chan time.Time, 1)
	c.OnPacketRTP(medi, selected, func(pkt *rtp.Packet) {
		au, e := decode(pkt)
		if e != nil {
			return
		}
		// Parameter sets, SEI and audio are not proof of a new picture.
		if hasVideoPicture(au, h265 != nil) {
			select {
			case frames <- time.Now().UTC():
			default:
			}
		}
	})
	if _, err = c.Play(nil); err != nil {
		return at, classify(err, "play_failed")
	}
	ended := make(chan error, 1)
	go func() { ended <- c.Wait() }()
	select {
	case at = <-frames:
		return at, ""
	case <-ctx.Done():
		return at, "video_timeout"
	case <-ended:
		return at, "video_unavailable"
	}
}

func (a *ArchiveService) recentVideo(req AvailabilityRequest, p AvailabilityProfile, now time.Time) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.captures {
		if c.plan.Tenant == req.Tenant && c.plan.Camera == req.Camera && c.plan.Source == req.Source && c.plan.ConnectionRevision == req.ConnectionRevision && c.plan.Profile == p.ID && c.plan.URL == p.URL && now.Before(c.plan.Expires) && !c.status.LastFrame.IsZero() && now.Sub(c.status.LastFrame) >= 0 && now.Sub(c.status.LastFrame) < 3*time.Second {
			return c.status.LastFrame, true
		}
	}
	return time.Time{}, false
}
func (g *Guard) reusedVideo(ctx context.Context, req AvailabilityRequest, p AvailabilityProfile) (time.Time, bool) {
	if archive, ok := g.opts.Archive.(*ArchiveService); ok {
		if at, yes := archive.recentVideo(req, p, time.Now()); yes {
			return at, true
		}
	}
	for _, path := range p.Paths {
		if !sourcePath.MatchString(path) {
			continue
		}
		g.sourceMu.Lock()
		identity := g.configured[path]
		g.sourceMu.Unlock()
		if identity != p.URL && identity != p.URL+"\x00browser" && identity != p.URL+"\x00browser\x00converted" {
			continue
		}
		if !g.pathReady(ctx, path) {
			continue
		}
		address := url.URL{Scheme: "rtsp", Host: g.opts.RTSPAddress, Path: "/" + path, User: url.UserPassword("service", g.opts.ServiceSecret)}
		local, cancel := context.WithTimeout(ctx, 2*time.Second)
		at, code := ProbeVideo(local, address.String())
		cancel()
		if code == "" {
			return at, true
		}
	}
	return time.Time{}, false
}
func (g *Guard) availability(w http.ResponseWriter, r *http.Request) {
	var req AvailabilityRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req) != nil || req.Tenant == "" || req.Camera == "" || req.Source == "" || len(req.Profiles) == 0 || len(req.Profiles) > 16 {
		http.Error(w, "invalid", 400)
		return
	}
	seen := map[string]bool{}
	for _, p := range req.Profiles {
		u, e := url.Parse(p.URL)
		if p.ID == "" || seen[p.ID] || e != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" {
			http.Error(w, "invalid profile", 400)
			return
		}
		seen[p.ID] = true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	stop := context.AfterFunc(g.runContext, cancel)
	defer stop()
	for _, p := range req.Profiles {
		if at, ok := g.reusedVideo(ctx, req, p); ok {
			archiveJSON(w, 200, AvailabilityResult{Profiles: []VideoObservation{{Profile: p.ID, Available: true, Complete: true, CheckedAt: time.Now().UTC(), LastVideoAt: &at}}})
			return
		}
	}
	// A bounded pool limits fan-out. A positive profile cancels siblings.
	jobs := make(chan AvailabilityProfile)
	var wg sync.WaitGroup
	var mu sync.Mutex
	result := AvailabilityResult{Profiles: []VideoObservation{}}
	for i := 0; i < min(4, len(req.Profiles)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				observation := VideoObservation{Profile: p.ID, Code: "monitor_capacity"}
				select {
				case g.availabilitySlots <- struct{}{}:
					if ctx.Err() == nil {
						probe, done := context.WithTimeout(ctx, 4*time.Second)
						at, code := ProbeVideo(probe, p.URL)
						done()
						observation.Complete = ctx.Err() == nil
						observation.Code = code
						observation.Available = code == ""
						if observation.Available {
							observation.LastVideoAt = &at
							observation.Complete = true
						}
					}
					<-g.availabilitySlots
				case <-ctx.Done():
				}
				observation.CheckedAt = time.Now().UTC()
				mu.Lock()
				result.Profiles = append(result.Profiles, observation)
				mu.Unlock()
				if observation.Available {
					cancel()
				}
			}
		}()
	}
	for _, p := range req.Profiles {
		select {
		case jobs <- p:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	archiveJSON(w, 200, result)
}
