package app_test

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"vms.local/backend/internal/app"
)

func TestRecordingAvailabilityUsesPlayablePartsAndHistoricalAuthorization(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	oldSite := b.call("POST", base+"/sites", map[string]string{"name": "Origem", "timezone": "America/Sao_Paulo"}, 201)["id"].(string)
	currentSite := b.call("POST", base+"/sites", map[string]string{"name": "Atual", "timezone": "America/New_York"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Histórico", "siteId": currentSite, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://127.0.0.1/fixture"}}}, 201)["id"].(string)
	db := cameraDeletionDB(t, b)
	insert := func(id, site string, ranges ...[2]string) {
		t.Helper()
		parts := []map[string]string{}
		for _, part := range ranges {
			parts = append(parts, map[string]string{"start": part[0], "end": part[1]})
		}
		raw, _ := json.Marshal(map[string]any{"parts": parts})
		_, err := db.Exec(context.Background(), `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata) SELECT $1,tenant_id,id,$4,source_version,'main',$5,$6,10,4096,$7 FROM cameras WHERE tenant_id=$2 AND id=$3`, id, tenant, camera, site, ranges[0][0], ranges[len(ranges)-1][1], raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("one", oldSite, [2]string{"2026-11-01T03:55:00Z", "2026-11-01T04:10:00Z"})
	insert("two", oldSite, [2]string{"2026-11-01T04:10:00Z", "2026-11-01T05:00:00Z"})
	insert("denied", currentSite, [2]string{"2026-11-01T05:00:00Z", "2026-11-01T06:00:00Z"})
	insert("three", oldSite, [2]string{"2026-11-01T06:00:00Z", "2026-11-01T07:00:00Z"})
	insert("inner-gap", oldSite, [2]string{"2026-11-01T07:10:00Z", "2026-11-01T07:12:00Z"}, [2]string{"2026-11-01T07:18:00Z", "2026-11-01T07:20:00Z"})
	insert("midnight-end", oldSite, [2]string{"2026-11-02T04:50:00Z", "2026-11-02T05:00:00Z"})
	insert("discarded", oldSite, [2]string{"2026-11-03T10:00:00Z", "2026-11-03T11:00:00Z"})
	if _, err := db.Exec(context.Background(), "UPDATE archive_segments SET available=false WHERE id='discarded'"); err != nil {
		t.Fatal(err)
	}
	viewerID := b.call("POST", "/admin/operators", map[string]string{"username": "archiveviewer", "password": "archiveviewer-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Somente origem", "grants": []app.Grant{
		{Action: "live", Scope: "camera", ResourceID: camera},
		{Action: "playback", Scope: "site", ResourceID: oldSite},
	}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+viewerID, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	viewer := independentBrowser(t, b, "archiveviewer", "archiveviewer-password", tenant)
	path := base + "/cameras/" + camera + "/recording-availability"
	if viewer.call("GET", path, nil, 200)["timezone"] != "America/New_York" {
		t.Fatal("timezone requires site administration or follows historical origin")
	}
	month := viewer.call("GET", path+"?month=2026-11", nil, 200)["days"].([]any)
	if len(month) != 1 || month[0] != "2026-11-01" {
		t.Fatalf("DST/month boundary, exclusive end, or discarded data is wrong: %v", month)
	}
	previousMonth := viewer.call("GET", path+"?month=2026-10", nil, 200)["days"].([]any)
	if len(previousMonth) != 1 || previousMonth[0] != "2026-10-31" {
		t.Fatal("cross-midnight part missing from previous month")
	}
	query := "?start=2026-11-01T03:00:00Z&end=2026-11-03T03:00:00Z"
	intervals := viewer.call("GET", path+query, nil, 200)["intervals"].([]any)
	if len(intervals) != 5 {
		t.Fatalf("expected adjacent merge, denied gap and internal gap: %v", intervals)
	}
	first := intervals[0].(map[string]any)
	sameInstant := func(value any, expected string) bool {
		actual, err := time.Parse(time.RFC3339Nano, value.(string))
		want, _ := time.Parse(time.RFC3339Nano, expected)
		return err == nil && actual.Equal(want)
	}
	if !sameInstant(first["start"], "2026-11-01T03:55:00Z") || !sameInstant(first["end"], "2026-11-01T05:00:00Z") {
		t.Fatalf("bad first continuous block: %v", first)
	}
	next := viewer.call("GET", path+"?direction=next&at=2026-11-01T04:00:00Z", nil, 200)["interval"].(map[string]any)
	if !sameInstant(next["start"], "2026-11-01T06:00:00Z") {
		t.Fatal("next selected a file within current block or exposed an unauthorized origin")
	}
	previous := viewer.call("GET", path+"?direction=previous&at=2026-11-01T06:30:00Z", nil, 200)["interval"].(map[string]any)
	if previous["start"] != first["start"] {
		t.Fatal("previous did not skip the current continuous block")
	}
	if viewer.call("GET", path+"?direction=previous&at=2026-10-01T00:00:00Z", nil, 200)["interval"] != nil {
		t.Fatal("nonexistent neighbour was not null")
	}
	viewer.call("GET", path+"?month=2026-13", nil, 422)
	viewer.call("GET", path+"?direction=other&at=2026-11-01T00:00:00Z", nil, 422)
	end := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC).Add(49 * time.Hour)
	viewer.call("GET", path+"?start=2026-11-01T03:00:00Z&end="+url.QueryEscape(end.Format(time.RFC3339)), nil, 422)
	// Keep membership but revoke playback: cached monthly colours must not grant access.
	liveOnly := b.call("POST", base+"/groups", map[string]any{"name": "Só ao vivo", "grants": []app.Grant{{Action: "live", Scope: "camera", ResourceID: camera}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+viewerID, map[string]any{"groups": []string{liveOnly}, "revision": 1}, 200)
	viewer.call("GET", path+"?month=2026-11", nil, 403)
	viewer.call("GET", path+query, nil, 403)
}
