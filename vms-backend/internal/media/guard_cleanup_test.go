package media

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfirmedTerminationStopsRetryingAndReusesControlConnections(t *testing.T) {
	var requests, connections atomic.Int64
	control := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
	}))
	control.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	control.Start()
	defer control.Close()
	g := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "private-test-secret"})
	g.AllowSource("camera")
	for i := range 12 {
		lease, err := g.Grant("tenant", "tab", "camera", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		g.leases[lease.Token].NativeID = fmt.Sprintf("native-%d", i)
		g.Revoke(lease.Token)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done
	if n := requests.Load(); n != 12 {
		t.Errorf("confirmed session termination repeated: requests=%d expected=12", n)
	}
	if n := connections.Load(); n > 2 {
		t.Fatalf("serial control commands opened %d TCP connections", n)
	}
}

func TestNotYetVisibleHandshakeIsStillTerminatedAfterRevocation(t *testing.T) {
	var calls atomic.Int64
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "not registered yet", 404)
			return
		}
		fmt.Fprint(w, "{}")
	}))
	defer control.Close()
	g := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "private-test-secret"})
	g.AllowSource("camera")
	lease, _ := g.Grant("tenant", "tab", "camera", time.Second)
	g.leases[lease.Token].NativeID = "handshake"
	g.Revoke(lease.Token)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done
	if n := calls.Load(); n != 2 {
		t.Fatalf("late handshake termination calls=%d expected=2", n)
	}
}

func TestMissingSessionsReuseBoundedConnectionPoolDuringHandshakeGrace(t *testing.T) {
	var connections, requests atomic.Int64
	control := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "session not found", 404)
	}))
	control.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	control.Start()
	defer control.Close()
	g := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "private-test-secret"})
	g.AllowSource("camera")
	for i := range 32 {
		lease, _ := g.Grant("tenant", "tab", "camera", 10*time.Millisecond)
		g.leases[lease.Token].NativeID = fmt.Sprintf("missing-%d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	time.Sleep(650 * time.Millisecond)
	cancel()
	<-done
	if requests.Load() < 64 || connections.Load() > 32 {
		t.Fatalf("handshake grace churned TCP sockets: requests=%d connections=%d", requests.Load(), connections.Load())
	}
}

func TestEstablishedSessionAlreadyClosedByPeerNeedsNoFurtherKick(t *testing.T) {
	var requests atomic.Int64
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "session not found", 404)
	}))
	defer control.Close()
	g := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "private-test-secret"})
	g.AllowSource("camera")
	lease, _ := g.Grant("tenant", "tab", "camera", time.Second)
	g.leases[lease.Token].NativeID = "native"
	if err := g.MarkEstablished(lease.Token); err != nil {
		t.Fatal(err)
	}
	g.Revoke(lease.Token)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done
	if requests.Load() != 1 {
		t.Fatal("already-established closed session repeatedly kicked")
	}
	if g.MarkEstablished(lease.Token) == nil {
		t.Fatal("late established acknowledgement revived a revoked lease")
	}
}
