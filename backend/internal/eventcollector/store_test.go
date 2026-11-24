package eventcollector

import (
	"bytes"
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/camerafixture"
)

func TestDurableTenantQueue(t *testing.T) {
	dir := t.TempDir()
	secret := strings.Repeat("x", 40)
	limits := map[string]int64{"north": 1100, "south": 1100}
	s, err := Open(dir, secret, 2200, limits)
	if err != nil {
		t.Fatal(err)
	}
	password := "private-fixture-camera-password"
	a := Assignment{Tenant: "north", Camera: "1", Revision: 1, Source: camera.Source{Type: "hikvision", Connection: &camera.Connection{Password: &password}}}
	b := a
	b.Tenant = "south"
	if err = s.configure([]Assignment{a, b}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	alert := &camera.CameraAlert{Kind: "intrusion", State: "active"}
	for i := 0; i < 12; i++ {
		if _, err = s.observe(a, alert, at); err != nil {
			t.Fatal(err)
		}
	}
	if full, err := s.observe(b, alert, at); full || err != nil {
		t.Fatal("tenant reserve lost", err)
	}
	batch, err := s.batch()
	if err != nil || !batch.Pressure {
		t.Fatal("pressure missing", err)
	}
	north, south, alarms := 0, 0, 0
	for _, r := range batch.Items {
		if r.Code == "queue_full" {
			alarms++
		}
		if r.Alert != nil {
			if r.Tenant == "north" {
				north++
			} else {
				south++
			}
			if !r.At.Equal(at) {
				t.Fatal("changed receive time")
			}
		}
	}
	if north == 0 || north >= 12 || south != 1 || alarms != 1 {
		t.Fatalf("unexpected queue %d/%d, saturation transitions %d", north, south, alarms)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "collector.db"))
	if err != nil || bytes.Contains(raw, []byte(password)) {
		t.Fatal("plaintext credential in queue")
	}
	s, err = Open(dir, secret, 2200, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.assignments()
	if err != nil || len(cfg) != 2 || *cfg[0].Source.Connection.Password != password {
		t.Fatal("lost configuration after restart", err)
	}
	replay, err := s.batch()
	if err != nil || len(replay.Items) != len(batch.Items) {
		t.Fatal("lost pending receipts", err)
	}
	ids := []string{}
	for i, r := range replay.Items {
		if r.ID != batch.Items[i].ID {
			t.Fatal("changed receipt identity")
		}
		ids = append(ids, r.ID)
	}
	if s.ack([]string{ids[0], "wrong"}) == nil {
		t.Fatal("invalid acknowledgement accepted")
	}
	if err = s.ack(ids); err != nil {
		t.Fatal("partial acknowledgement was not rolled back", err)
	}
	after, err := s.batch()
	if err != nil || len(after.Items) != 1 || after.Items[0].Condition != "queue" || after.Items[0].Code != "" {
		t.Fatal("queue recovery not recorded", err)
	}
	if err = s.ack([]string{after.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	after, err = s.batch()
	if err != nil || len(after.Items) != 0 || after.Bytes != 0 {
		t.Fatal("queue not drained", err)
	}
}

func TestWorkerResumesWithoutCore(t *testing.T) {
	device := httptest.NewServer(&camerafixture.Device{Interval: time.Hour})
	defer device.Close()
	location, _ := url.Parse(device.URL)
	port, _ := strconv.Atoi(location.Port())
	password := "fixture"
	a := Assignment{Tenant: "tenant", Camera: "camera", Revision: 1, Source: camera.Source{Type: "hikvision", Connection: &camera.Connection{Host: location.Hostname(), Protocol: "http", HTTPPort: port, Username: "fixture", Password: &password}}}
	dir := t.TempDir()
	open := func() (*Store, *Worker, func()) {
		store, err := Open(dir, strings.Repeat("x", 40), 1<<20, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, stop := context.WithCancel(context.Background())
		worker, err := Start(ctx, store)
		if err != nil {
			stop()
			store.Close()
			t.Fatal(err)
		}
		return store, worker, func() { stop(); worker.Close(); store.Close() }
	}
	wait := func(store *Store, alerts int) Batch {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			b, err := store.batch()
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, r := range b.Items {
				if r.Alert != nil {
					n++
				}
			}
			if n >= alerts {
				return b
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("missing %d offline receipts", alerts)
		return Batch{}
	}
	first := func() Batch {
		store, worker, stop := open()
		defer stop()
		if err := worker.apply([]Assignment{a}); err != nil {
			t.Fatal(err)
		}
		return wait(store, 4)
	}()
	store, _, stop := open()
	defer stop()
	replay := wait(store, 8)
	for i, r := range first.Items {
		if replay.Items[i].ID != r.ID {
			t.Fatal("lost offline receipt")
		}
	}
	gap, restored := false, false
	for _, r := range replay.Items[len(first.Items):] {
		if r.Condition == "stream" {
			if r.Code == "coverage_uncertain" {
				gap = true
			}
			if r.Code == "" && gap {
				restored = true
			}
		}
	}
	if !restored {
		t.Fatal("restart coverage interval was lost")
	}
}
