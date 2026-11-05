package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"time"
)

type sourceConverter struct {
	process *OwnedProcess
	done    chan struct{}
	touched time.Time
}

// SetSourceMode shares a prepared path across readers. Only the browser variant
// is converted; capture always reads the immutable original source.
func (g *Guard) SetSourceMode(ctx context.Context, path, address string, compatible, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	stop := context.AfterFunc(g.runContext, cancel)
	defer stop()
	if !sourcePath.MatchString(path) {
		return errors.New("invalid source path")
	}
	u, e := url.Parse(address)
	if e != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Host == "" {
		return errors.New("invalid RTSP source")
	}
	for {
		g.sourceMu.Lock()
		if waiting := g.preparations[path]; waiting != nil {
			g.sourceMu.Unlock()
			select {
			case <-waiting:
				continue
			case <-ctx.Done():
				return errors.New("source preparation timed out")
			}
		}
		done := make(chan struct{})
		g.preparations[path] = done
		g.sourceMu.Unlock()
		defer func() { g.sourceMu.Lock(); delete(g.preparations, path); close(done); g.sourceMu.Unlock() }()
		break
	}

	return g.prepareSourceMode(ctx, path, address, compatible, force)
}
func (g *Guard) prepareSourceMode(ctx context.Context, path, address string, compatible, force bool) error {
	identity := address
	if compatible {
		identity += "\x00browser"
	}
	if force {
		identity += "\x00converted"
	}
	g.sourceMu.Lock()
	previous, configured := g.configured[path]
	if configured && previous != identity {
		g.sourceMu.Unlock()
		return errors.New("immutable source conflict")
	}
	if configured {
		if c := g.converters[path]; c != nil {
			select {
			case <-c.done:
				delete(g.converters, path)
			default:
				c.touched = time.Now()
				g.sourceMu.Unlock()
				return nil
			}
		} else if !g.converted[path] {
			g.sourceMu.Unlock()
			return nil
		}
	}
	convert := force || g.converted[path]
	g.sourceMu.Unlock()
	select {
	case g.prepareSlots <- struct{}{}:
		defer func() { <-g.prepareSlots }()
	default:
		return errors.New("source preparation capacity exhausted")
	}
	if compatible && !convert {
		result, e := Probe(ctx, address)
		if e != nil {
			return errors.New("source probe failed")
		}
		convert = !result.H264Candidate
	}
	if !convert {
		if e := g.configureSource(ctx, path, map[string]any{"source": address, "rtspTransport": "tcp", "sourceOnDemand": true, "sourceOnDemandCloseAfter": "1s"}); e != nil {
			return e
		}
		g.sourceMu.Lock()
		g.configured[path] = identity
		g.sourceMu.Unlock()
		return nil
	}
	host, _, e := net.SplitHostPort(g.opts.RTSPAddress)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("private RTSP publisher unavailable")
	}
	g.sourceMu.Lock()
	if len(g.converters) >= g.opts.MaxLiveConverters {
		g.sourceMu.Unlock()
		return errors.New("live conversion capacity exhausted")
	}
	c := &sourceConverter{done: make(chan struct{}), touched: time.Now()}
	g.converters[path] = c
	g.sourceMu.Unlock()
	prepared := false
	defer func() {
		if !prepared {
			g.sourceMu.Lock()
			if c.process != nil {
				c.process.Stop()
			}
			if g.converters[path] == c {
				delete(g.converters, path)
			}
			if g.converted[path] {
				g.removeConvertedSource(path)
			}
			g.sourceMu.Unlock()
		}
	}()
	if !configured {
		if e = g.configureSource(ctx, path, map[string]any{"source": "publisher", "overridePublisher": false}); e != nil {
			return e
		}
	}
	g.sourceMu.Lock()
	g.configured[path] = identity
	g.converted[path] = true
	g.sourceMu.Unlock()
	publish := url.URL{Scheme: "rtsp", Host: g.opts.RTSPAddress, Path: "/" + path, User: url.UserPassword("service", g.opts.ServiceSecret)}
	ffmpeg := g.opts.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	cmd := exec.CommandContext(g.runContext, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-rtsp_transport", "tcp", "-timeout", "5000000", "-threads", "2", "-i", address, "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "baseline", "-crf", "23", "-bf", "0", "-g", "30", "-keyint_min", "15", "-rtsp_transport", "tcp", "-f", "rtsp", publish.String())
	// Device URLs and the private publishing password never reach logs.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	process, e := StartOwned(cmd)
	if e != nil {
		return errors.New("live converter unavailable")
	}
	g.sourceMu.Lock()
	c.process = process
	g.sourceMu.Unlock()
	go func() { cmd.Wait(); process.Stop(); close(c.done) }()
	readyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if g.pathReady(readyCtx, path) {
			g.sourceMu.Lock()
			c.touched = time.Now()
			g.sourceMu.Unlock()
			prepared = true
			return nil
		}
		select {
		case <-readyCtx.Done():
			return errors.New("live conversion startup timed out")
		case <-c.done:
			return errors.New("live conversion failed")
		case <-ticker.C:
		}
	}
}
func (g *Guard) configureSource(ctx context.Context, path string, config map[string]any) error {
	raw, _ := json.Marshal(config)
	req, err := http.NewRequestWithContext(ctx, "POST", g.opts.ControlURL+"/v3/config/paths/add/"+path, bytes.NewReader(raw))
	if err != nil {
		return errors.New("invalid control request")
	}
	req.SetBasicAuth("service", g.opts.ServiceSecret)
	req.Header.Set("Content-Type", "application/json")
	res, err := g.client.Do(req)
	if err != nil {
		return errors.New("media source configuration failed")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != 200 {
		return errors.New("media source configuration failed")
	}
	return g.AllowSource(path)
}
func (g *Guard) pathReady(ctx context.Context, path string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", g.opts.ControlURL+"/v3/paths/get/"+url.PathEscape(path), nil)
	if err != nil {
		return false
	}
	req.SetBasicAuth("service", g.opts.ServiceSecret)
	res, err := g.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var data struct{ Ready bool }
	err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&data)
	return res.StatusCode == 200 && err == nil && data.Ready
}
func (g *Guard) cleanupConverters(now time.Time) {
	// Lifecycle cleanup never blocks lease revocation or a preparation in progress.
	if !g.sourceMu.TryLock() {
		return
	}
	defer g.sourceMu.Unlock()
	active := map[string]bool{}
	g.mu.Lock()
	for _, l := range g.leases {
		if !l.Revoked && now.Before(l.Expires) {
			active[l.Path] = true
		}
	}
	g.mu.Unlock()
	for path, c := range g.converters {
		if g.preparations[path] != nil {
			continue
		}
		if active[path] {
			c.touched = now
			continue
		}
		if now.Sub(c.touched) < 3*time.Second {
			continue
		}
		c.process.Stop()
		delete(g.converters, path)
		// Configuration remains private and reusable; restart on the next prepare.
		g.removeConvertedSource(path)
	}
}
func (g *Guard) closeConverters() {
	g.cancel()
	g.sourceMu.Lock()
	defer g.sourceMu.Unlock()
	for _, c := range g.converters {
		if c.process != nil {
			c.process.Stop()
		}
	}
	g.converters = map[string]*sourceConverter{}
}

// Called under sourceMu. Keep tracked configuration after a failed API delete,
// allowing retry against the existing private publisher rather than add-conflict.
func (g *Guard) removeConvertedSource(path string) {
	req, e := http.NewRequest("DELETE", g.opts.ControlURL+"/v3/config/paths/delete/"+url.PathEscape(path), nil)
	if e != nil {
		return
	}
	req.SetBasicAuth("service", g.opts.ServiceSecret)
	res, e := g.client.Do(req)
	if e != nil {
		return
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != 200 && res.StatusCode != 404 {
		return
	}
	delete(g.configured, path)
	delete(g.converted, path)
	g.mu.Lock()
	delete(g.sources, path)
	g.mu.Unlock()
}

func (g *Guard) runConverterCleanup(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			g.cleanupConverters(now)
		}
	}
}
