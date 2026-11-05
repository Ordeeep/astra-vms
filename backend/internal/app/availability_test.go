package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vms.local/backend/internal/media"
)

func availabilityFixture(t *testing.T) (*availabilityMonitor, *availabilityEntry, Camera, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	c := Camera{ID: "camera", Name: "Entrada", SiteID: "site", Active: true, EffectiveActive: true, SourceVersion: "source", ConnectionRevision: 1, Profiles: []Profile{{ID: "main", Name: "Principal"}, {ID: "sub1", Name: "Sub"}}}
	m := newAvailabilityMonitor()
	m.reconcile([]availabilityTarget{{"tenant", c}}, now)
	return m, m.entries["tenant/camera"], c, now
}
func videoResult(profile string, at time.Time) media.AvailabilityResult {
	return media.AvailabilityResult{Profiles: []media.VideoObservation{{Profile: profile, Available: true, Complete: true, LastVideoAt: &at, CheckedAt: at}}}
}
func failedProfiles(ids ...string) media.AvailabilityResult {
	r := media.AvailabilityResult{}
	for _, id := range ids {
		r.Profiles = append(r.Profiles, media.VideoObservation{Profile: id, Complete: true, Code: "video_timeout"})
	}
	return r
}
func TestAvailabilityConsecutiveCompleteFailuresAndRecovery(t *testing.T) {
	m, e, c, now := availabilityFixture(t)
	m.apply(e, videoResult("sub1", now), "", now)
	if e.status.State != "online" {
		t.Fatal("one profile must suffice")
	}
	m.apply(e, failedProfiles("main", "sub1"), "", now.Add(30*time.Second))
	if e.status.State != "online" || e.failures != 1 || !e.status.LastVideoAt.Equal(now) {
		t.Fatal("single failure must retain prior confirmed state and video time")
	}
	m.apply(e, failedProfiles("main", "sub1"), "", now.Add(60*time.Second))
	if e.status.State != "offline" || e.status.Profile != "" {
		t.Fatal("two complete camera cycles must be offline")
	}
	m.apply(e, videoResult("main", now.Add(61*time.Second)), "", now.Add(61*time.Second))
	if e.status.State != "online" || e.failures != 0 || e.status.Profile != "main" {
		t.Fatal("first good profile must recover immediately")
	}
	if len(e.status.Warnings) != 1 || e.status.Warnings[0].Profile != "sub1" {
		t.Fatal("clear recovered profile only")
	}
	m.reconcile([]availabilityTarget{{"tenant", c}}, now.Add(61*time.Second))
	if m.snapshot("tenant", c, now.Add(61*time.Second)).State != "online" {
		t.Fatal("snapshot")
	}
}
func TestAvailabilityIncompleteAndLocalFailuresNeverCountOffline(t *testing.T) {
	m, e, _, now := availabilityFixture(t)
	m.apply(e, failedProfiles("main", "sub1"), "", now)
	if e.failures != 1 || e.status.State != "unknown" {
		t.Fatal("first failure without prior success is unknown")
	}
	m.apply(e, failedProfiles("main"), "", now.Add(30*time.Second))
	if e.failures != 0 || e.status.State != "unknown" {
		t.Fatal("partial profile checks must break failure streak")
	}
	m.apply(e, failedProfiles("main", "sub1"), "", now.Add(60*time.Second))
	if e.status.State == "offline" {
		t.Fatal("partial cycle cannot join two separated failures")
	}
	m.apply(e, media.AvailabilityResult{}, "worker_unavailable", now.Add(70*time.Second))
	if e.status.State != "unknown" || e.failures != 0 {
		t.Fatal("worker error is not source failure")
	}
}
func TestAvailabilityRejectsOldRevisionAndCancelsRemovedCameras(t *testing.T) {
	m, e, c, now := availabilityFixture(t)
	m.apply(e, videoResult("main", now), "", now)
	c.Name = "Renomeada"
	c.Revision = 55
	m.reconcile([]availabilityTarget{{"tenant", c}}, now)
	if m.entries["tenant/camera"] != e || m.snapshot("tenant", c, now).State != "online" {
		t.Fatal("rename must retain availability")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	c.ConnectionRevision++
	if m.snapshot("tenant", c, now).State != "unknown" {
		t.Fatal("reads must reject old configuration before reconciliation")
	}
	m.reconcile([]availabilityTarget{{"tenant", c}}, now)
	if ctx.Err() == nil {
		t.Fatal("editing must cancel old test")
	}
	replacement := m.entries["tenant/camera"]
	m.apply(e, videoResult("main", now), "", now)
	if replacement.status.State != "unknown" {
		t.Fatal("late success cannot mutate replacement entry")
	}
	removed, cancelRemoved := context.WithCancel(context.Background())
	replacement.cancel = cancelRemoved
	m.reconcile(nil, now)
	if removed.Err() == nil || len(m.entries) != 0 {
		t.Fatal("deletion must cancel and discard")
	}
	m.apply(replacement, videoResult("main", now), "", now)
	if len(m.entries) != 0 {
		t.Fatal("late result must not resurrect deletion")
	}
}
func TestAvailabilityStalenessSuspensionAndTenantIsolation(t *testing.T) {
	m, e, c, now := availabilityFixture(t)
	m.apply(e, videoResult("main", now), "", now)
	if m.snapshot("other-tenant", c, now).State != "unknown" {
		t.Fatal("cross-tenant state leak")
	}
	later := now.Add(66 * time.Second)
	m.reconcile([]availabilityTarget{{"tenant", c}}, later)
	if m.snapshot("tenant", c, later).State != "unknown" {
		t.Fatal("expired result must become unknown")
	}
	m.apply(e, failedProfiles("main", "sub1"), "", later)
	if e.status.State != "unknown" || e.failures != 1 {
		t.Fatal("stale success cannot be retained through new failure")
	}
	c.EffectiveActive = false
	m.reconcile([]availabilityTarget{{"tenant", c}}, later)
	if m.snapshot("tenant", c, later).State != "suspended" {
		t.Fatal("disabled camera must be suspended")
	}
	c.EffectiveActive = true
	m.reconcile([]availabilityTarget{{"tenant", c}}, later)
	if m.snapshot("tenant", c, later).State != "unknown" {
		t.Fatal("reactivation needs fresh evidence")
	}
	if m.snapshot("tenant", c, later.Add(6*time.Second)).Code != "monitor_unavailable" {
		t.Fatal("lost inventory heartbeat must be neutral")
	}
}
func TestAvailabilitySanitizesDiagnosticsAndRejectsMetadataOnlySuccess(t *testing.T) {
	m, e, _, now := availabilityFixture(t)
	r := failedProfiles("main")
	r.Profiles[0].Code = "rtsp://user:secret@private/video"
	r.Profiles = append(r.Profiles, media.VideoObservation{Profile: "sub1", Available: true, Complete: true})
	m.apply(e, r, "", now)
	raw, _ := json.Marshal(e.status)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "rtsp://") || e.status.State == "online" || e.status.State == "offline" {
		t.Fatal("must reject unproven success and redact diagnostics")
	}
}
