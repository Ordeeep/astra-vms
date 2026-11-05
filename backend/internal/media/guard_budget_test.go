package media

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpiredMediaBatchHasGlobalTerminationBudget(t *testing.T) {
	var terminated atomic.Bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(200)
		}
	}))
	defer control.Close()
	g := NewGuard(GuardOptions{ControlURL: control.URL, ServiceSecret: "test-secret", OnControlFailure: func() { terminated.Store(true) }})
	g.AllowSource("camera")
	for i := 0; i < 100; i++ {
		lease, err := g.Grant("tenant", "context", "camera", 100*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		g.leases[lease.Token].NativeID = fmt.Sprintf("native-%d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)
	deadline := time.Now().Add(600 * time.Millisecond)
	for !terminated.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !terminated.Load() {
		t.Fatal("slow batch left sessions active beyond its global termination budget")
	}
}
