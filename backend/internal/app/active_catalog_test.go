package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/media"
	"vms.local/backend/internal/storage"
)

func TestActiveCatalogGrowthIsMonotonicAndScoped(t *testing.T) {
	var catalog atomic.Value
	catalog.Store([]storage.Entry{})
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/catalog" {
			after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			items := []storage.Entry{}
			for _, item := range catalog.Load().([]storage.Entry) {
				if item.Sequence > after {
					items = append(items, item)
				}
			}
			json.NewEncoder(w).Encode(items)
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		w.WriteHeader(204)
	}))
	defer worker.Close()
	b := newTestAPI(t, func(options *app.Options) {
		options.WorkerURL = worker.URL
		options.WorkerSecret = "catalog-test-private-secret"
	})
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Catalog test", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Growing camera", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://127.0.0.1/catalog-fixture"}}}, 201)["id"].(string)
	db := cameraDeletionDB(t, b)
	key := cameraTestKey(t, db, tenant, camera)
	events, closeEvents := openEvents(t, b, tenant, "")
	defer closeEvents()
	epoch := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	publish := func(sequence int64, seconds int, deleted bool) {
		parts := []media.PartIndex{}
		for i := 0; i < seconds; i++ {
			parts = append(parts, media.PartIndex{ArchivePart: media.ArchivePart{Start: epoch.Add(time.Duration(i) * time.Second), End: epoch.Add(time.Duration(i+1) * time.Second)}})
		}
		index := media.SegmentIndex{Tenant: tenant, Camera: key, Site: site, Source: "source", Profile: "main", Start: epoch, End: epoch.Add(time.Duration(seconds) * time.Second), Parts: parts}
		raw, _ := json.Marshal(index)
		catalog.Store([]storage.Entry{{ID: "growing-object", Tenant: tenant, Kind: storage.Recording, Size: int64(seconds * 100), Charged: 4096, Metadata: raw, Sequence: sequence, Deleted: deleted}})
	}
	wait := func(sequence int64, seconds int, available bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var revision, size int64
			var end time.Time
			var present bool
			err := db.QueryRow(context.Background(), "SELECT catalog_sequence,bytes,end_at,available FROM archive_segments WHERE id='growing-object'").Scan(&revision, &size, &end, &present)
			if err == nil && revision == sequence && size == int64(seconds*100) && end.Equal(epoch.Add(time.Duration(seconds)*time.Second)) && present == available {
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatalf("catalog did not reach revision=%d seconds=%d available=%t", sequence, seconds, available)
	}
	publish(1, 1, false)
	wait(1, 1, true)
	publish(2, 2, false)
	wait(2, 2, true)
	// The event must identify its camera and carry the changed interval.
	found := false
	deadline := time.After(4 * time.Second)
	for !found {
		select {
		case event := <-events:
			if event.Type == "ArchiveChanged" {
				var payload map[string]any
				json.Unmarshal(event.Payload, &payload)
				if event.CameraID != camera || payload["end"] == nil {
					t.Fatal("archive event missing camera/window", event)
				}
				found = true
			}
		case <-deadline:
			t.Fatal("no archive event")
		}
	}
	publish(1, 1, false)
	time.Sleep(600 * time.Millisecond)
	wait(2, 2, true)
	// A newer envelope cannot shrink existing footage.
	publish(3, 1, false)
	time.Sleep(600 * time.Millisecond)
	wait(2, 2, true)
	publish(4, 2, true)
	wait(4, 2, false)
	publish(5, 3, false)
	time.Sleep(600 * time.Millisecond)
	wait(4, 2, false)
	// A slow but active source reports real delay, without extrapolating blue.
	lateEnd := time.Now().UTC().Add(-8 * time.Second)
	lateStart := lateEnd.Add(-8 * time.Second)
	raw, _ := json.Marshal(map[string]any{"parts": []map[string]time.Time{{"start": lateStart, "end": lateEnd}}})
	_, err := db.Exec(context.Background(), `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata)
 VALUES('slow-source',$1,$2,$3,'source','main',$4,$5,100,4096,$6)`, tenant, key, site, lateStart, lateEnd, raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(context.Background(), `INSERT INTO recording_status(tenant_id,camera_id,code,last_frame,last_write) VALUES($1,$2,'recording',now(),now())
 ON CONFLICT(tenant_id,camera_id) DO UPDATE SET code='recording',last_frame=now(),last_write=now()`, tenant, key)
	if err != nil {
		t.Fatal(err)
	}
	query := "?start=" + lateStart.Add(-time.Second).Format(time.RFC3339Nano) + "&end=" + time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	availability := b.call("GET", base+"/cameras/"+camera+"/recording-availability"+query, nil, 200)
	if availability["sourceDelaySeconds"].(float64) < 8 {
		t.Fatal("slow source delay not reported", availability)
	}
	intervals := availability["intervals"].([]any)
	if len(intervals) != 1 {
		t.Fatal("slow source availability missing", availability)
	}
	if intervals[0].(map[string]any)["end"] != lateEnd.Format(time.RFC3339Nano) {
		// PostgreSQL timestamps retain microseconds, while Go has nanoseconds.
		actual, _ := time.Parse(time.RFC3339Nano, intervals[0].(map[string]any)["end"].(string))
		if actual.Sub(lateEnd) > time.Microsecond || lateEnd.Sub(actual) > time.Microsecond {
			t.Fatal("availability extrapolated beyond recorded footage", availability)
		}
	}
}
