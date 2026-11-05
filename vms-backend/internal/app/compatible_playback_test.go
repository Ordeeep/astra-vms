package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompatibleArchiveKeepsAuthorizationAndRanges(t *testing.T) {
	var calls atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/archive/objects/object/compatible" || r.URL.Query().Get("tenant") != "tenant" || r.URL.Query().Get("part") != "2" || r.URL.Query().Get("kind") != "data" || r.Header.Get("Authorization") != "Bearer worker-test" || r.Header.Get("Range") != "bytes=1-3" {
			t.Error("lost authorized part or range")
		}
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Content-Range", "bytes 1-3/5")
		w.WriteHeader(206)
		w.Write([]byte("bcd"))
	}))
	defer worker.Close()
	a := &App{opts: Options{WorkerURL: worker.URL, WorkerSecret: "worker-test"}}
	var permitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.proxyCompatibleArchive(w, r, "tenant", "object", 2, "data", func(context.Context) bool { return permitted.Load() })
	}))
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Range", "bytes=1-3")
	res, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 || calls.Load() != 0 {
		t.Fatal("denied request reached codec conversion")
	}
	permitted.Store(true)
	res, e = server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 206 || string(body) != "bcd" || res.Header.Get("Content-Range") != "bytes 1-3/5" || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("converted range was not preserved")
	}
}

func TestCompatibleArchiveCancelsPreparationAfterRevocation(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(cancelled) }))
	defer worker.Close()
	a := &App{opts: Options{WorkerURL: worker.URL}}
	var permitted atomic.Bool
	permitted.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.proxyCompatibleArchive(w, r, "tenant", "object", 0, "init", func(context.Context) bool { return permitted.Load() })
	}))
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := server.Client().Get(server.URL)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	<-started
	permitted.Store(false)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked preparation continued in worker")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revoked response remained open")
	}
}
