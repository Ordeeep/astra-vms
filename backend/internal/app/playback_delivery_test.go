package app

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRevocationStopsDeliveryToClientThatDoesNotDrainSocket(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "67108864")
		w.WriteHeader(206)
		chunk := make([]byte, 32768)
		for range 2048 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer worker.Close()
	a := &App{opts: Options{WorkerURL: worker.URL}}
	var allowed atomic.Bool
	allowed.Store(true)
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		a.proxyArchiveBytes(w, r, "tenant", "object", 0, 67108864, func(context.Context) bool { return allowed.Load() })
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(*net.TCPConn).SetReadBuffer(1024)
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	reader := bufio.NewReader(conn)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "\r\n" {
			break
		}
	}
	// Keep the socket open but deliberately stop reading the response body.
	allowed.Store(false)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked delivery remained blocked behind downstream write")
	}
}
