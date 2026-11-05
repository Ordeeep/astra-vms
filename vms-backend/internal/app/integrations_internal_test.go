package app

import (
	"context"
	"testing"
	"vms.local/backend/internal/camera"
)

func TestManagedSourceOfflineSnapshotAndCancellation(t *testing.T) {
	password := "fixture-only"
	stored := camera.Source{Type: "dahua", Connection: &camera.Connection{Host: "127.0.0.1", Protocol: "http", HTTPPort: 1, RTSPPort: 8554, Username: "camera", Password: &password}, Profiles: []Profile{{ID: "main", Name: "Principal"}, {ID: "sub1", Name: "Conhecido"}}}
	request := camera.Source{Type: "dahua", Connection: &camera.Connection{Host: "127.0.0.1", Protocol: "http", HTTPPort: 1, RTSPPort: 9999, Username: "camera", Password: &password, ProfileStatus: "discovered"}, Profiles: []Profile{{ID: "main", Name: "Inventado"}, {ID: "sub99", Name: "Inventado"}}}
	a := &App{probeSlots: make(chan struct{}, 4)}
	known, e := a.prepareSource(context.Background(), request, &stored)
	if e != nil || known.Connection.RTSPPort != 8554 || known.Connection.ProfileStatus != "known" || len(known.Profiles) != 2 || known.Profiles[1].ID != "sub1" {
		t.Fatalf("legacy snapshot was lost or overridden: %v", e)
	}
	pending, e := a.prepareSource(context.Background(), request, nil)
	if e != nil || pending.Connection.RTSPPort != 0 || pending.Connection.ProfileStatus != "pending" || len(pending.Profiles) != 0 {
		t.Fatalf("new offline source invented transport/profiles: %v", e)
	}
	if _, e = camera.Resolve(pending, "main"); camera.Code(e) != "camera_discovery_required" {
		t.Fatal("pending source became a video origin")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = a.prepareSource(ctx, request, &stored); camera.Code(e) != "camera_api_timeout" {
		t.Fatal("cancelled request was accepted for persistence")
	}
}
