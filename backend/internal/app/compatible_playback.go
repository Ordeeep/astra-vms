package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Conversion occurs in the worker. The core keeps the same short-lived session,
// origin permissions and revocation checks as delivery of original bytes.
func (a *App) proxyCompatibleArchive(w http.ResponseWriter, r *http.Request, tenant, object string, part int, kind string, authorized func(context.Context) bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if !authorized(ctx) {
		problem(w, 403, "forbidden")
		return
	}
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
	query := url.Values{"tenant": {tenant}, "part": {strconv.Itoa(part)}, "kind": {kind}}
	req, err := http.NewRequestWithContext(ctx, "GET", a.opts.WorkerURL+"/archive/objects/"+url.PathEscape(object)+"/compatible?"+query.Encode(), nil)
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.opts.WorkerSecret)
	if value := r.Header.Get("Range"); value != "" {
		req.Header.Set("Range", value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		problem(w, 503, "media_unavailable")
		return
	}
	defer res.Body.Close()
	if !authorized(ctx) {
		problem(w, 403, "forbidden")
		return
	}
	if res.StatusCode == 416 {
		w.Header().Set("Content-Range", res.Header.Get("Content-Range"))
		w.WriteHeader(416)
		return
	}
	if res.StatusCode == 404 || res.StatusCode == 410 {
		problem(w, 410, "recording_unavailable")
		return
	}
	if res.StatusCode == 429 || res.StatusCode == 503 {
		w.Header().Set("Retry-After", "1")
		problem(w, 503, "video_preparation_busy")
		return
	}
	if (res.StatusCode != 200 && res.StatusCode != 206) || res.ContentLength <= 0 || res.ContentLength > 32<<20 {
		problem(w, 502, "video_preparation_failed")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	if res.StatusCode == 206 {
		w.Header().Set("Content-Range", res.Header.Get("Content-Range"))
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
	w.WriteHeader(res.StatusCode)
	remaining := res.ContentLength
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
	if controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
}
