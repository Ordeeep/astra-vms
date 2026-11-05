package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

type streamEvent struct {
	EventID    string          `json:"eventId"`
	ContextID  string          `json:"contextId"`
	TenantID   string          `json:"tenantId"`
	ResourceID string          `json:"resourceId"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

func openEvents(t *testing.T, b *browserClient, tenant, cursor string) (<-chan streamEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", b.server+"/api/v1/tenants/"+tenant+"/events?context="+b.view, nil)
	req.Header.Set("Last-Event-ID", cursor)
	res, err := b.client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		cancel()
		t.Fatalf("event stream status %d", res.StatusCode)
	}
	out := make(chan streamEvent, 32)
	go func() {
		defer close(out)
		defer res.Body.Close()
		scan := bufio.NewScanner(res.Body)
		for scan.Scan() {
			if !strings.HasPrefix(scan.Text(), "data: ") {
				continue
			}
			var e streamEvent
			if json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &e) != nil {
				return
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, func() { cancel(); res.Body.Close() }
}
func nextEvent(t *testing.T, ch <-chan streamEvent, kind string) streamEvent {
	t.Helper()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatal("event stream ended")
			}
			if e.Type == kind {
				return e
			}
		case <-timeout.C:
			t.Fatalf("missing %s", kind)
		}
	}
}

func TestEventsEnforceScopeCurrentGrantsAndContextRevocation(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Foreign"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]any{"name": "SSE", "timezone": "UTC"}, 201)["id"].(string)
	camera := func(name string) string {
		return b.call("POST", base+"/cameras", map[string]any{"name": name, "siteId": site, "profiles": []any{map[string]string{"id": "main", "name": "Main", "url": "rtsp://secret:never-return@127.0.0.1/camera"}}}, 201)["id"].(string)
	}
	allowed, denied := camera("Allowed"), camera("Denied")
	grants := []app.Grant{{Action: "live", Scope: "camera", ResourceID: allowed}}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Restricted", "grants": grants}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "events", "password": "events-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	c := *b
	client := *b.client
	client.Jar, _ = cookiejar.New(nil)
	c.client = &client
	c.login("events", "events-password")
	c.selectTenant(tenant)
	stream, closeStream := openEvents(t, &c, tenant, "")
	defer closeStream()
	first := nextEvent(t, stream, "ResyncRequired")
	if first.ContextID != c.view || first.TenantID != tenant {
		t.Fatal("wrong context envelope")
	}
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	outbox := pgx.Identifier{b.options.Schema, "event_outbox"}.Sanitize()
	// A foreign tenant's event and an ungranted camera precede an allowed event.
	_, err = db.Exec(context.Background(), "INSERT INTO "+outbox+"(tenant_id,resource_id,camera_id,site_id,revision,kind) VALUES($1,'foreign',$3,$4,1,'CameraStatusChanged'),($2,$5,$5,$4,1,'CameraStatusChanged')", other, tenant, allowed, site, denied)
	if err != nil {
		t.Fatal(err)
	}
	b.call("PATCH", base+"/cameras/"+allowed, map[string]any{"name": "Allowed changed", "active": true, "revision": 1}, 200)
	e := nextEvent(t, stream, "CameraStatusChanged")
	if e.ResourceID != allowed || e.TenantID != tenant {
		t.Fatalf("event crossed scope: %+v", e)
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "rtsp:") {
		t.Fatal("source leaked")
	}
	// Keep the membership but remove its grants; the active stream must notice.
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{}, "revision": 1}, 200)
	nextEvent(t, stream, "AuthorizationChanged")
	b.call("PATCH", base+"/cameras/"+allowed, map[string]any{"name": "Now unauthorized", "active": true, "revision": 2}, 200)
	timer := time.NewTimer(700 * time.Millisecond)
	select {
	case e := <-stream:
		if e.Type == "CameraStatusChanged" {
			t.Fatal("revoked event delivered")
		}
	case <-timer.C:
	}
	timer.Stop()
	began := time.Now()
	c.call("DELETE", "/contexts/"+c.view, nil, 204)
	nextEvent(t, stream, "ContextClosed")
	if time.Since(began) > 2*time.Second {
		t.Fatal("context revocation exceeded two seconds")
	}
}

func TestEventDeliveryOrderingSurvivesOutOfOrderTransactionCommit(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	stream, stop := openEvents(t, b, tenant, "")
	defer stop()
	nextEvent(t, stream, "ResyncRequired")
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	table := pgx.Identifier{b.options.Schema, "event_outbox"}.Sanitize()
	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(context.Background(), "INSERT INTO "+table+"(tenant_id,resource_id,revision,kind) VALUES($1,'commits-later',1,'StoragePressure')", tenant)
	if err != nil {
		t.Fatal(err)
	}
	second, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	_, err = second.Exec(context.Background(), "INSERT INTO "+table+"(tenant_id,resource_id,revision,kind) VALUES($1,'commits-first',1,'StoragePressure')", tenant)
	if err != nil {
		t.Fatal(err)
	}
	first := nextEvent(t, stream, "StoragePressure")
	if first.ResourceID != "commits-first" {
		t.Fatal("uncommitted event delivered")
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := nextEvent(t, stream, "StoragePressure")
	if last.ResourceID != "commits-later" || last.EventID == first.EventID {
		t.Fatal("late commit was lost")
	}
	stop()
	resumed, closeResumed := openEvents(t, b, tenant, first.EventID)
	defer closeResumed()
	nextEvent(t, resumed, "Connected")
	if nextEvent(t, resumed, "StoragePressure").ResourceID != "commits-later" {
		t.Fatal("resume missed committed event")
	}
}

func TestIdleHTTP2EventsRemainConnectedAndCancellationDoesNotCrashCore(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	events, stop := openEvents(t, b, tenant, "")
	defer stop()
	nextEvent(t, events, "ResyncRequired")
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("idle event stream closed at the write deadline")
		}
	case <-timer.C:
	}
	b.call("DELETE", "/contexts/"+b.view, nil, 204)
	nextEvent(t, events, "ContextClosed")
	stop()
	b.call("GET", "/health", nil, 200)
}

func TestQueuedCameraEventUsesCurrentSiteButArchiveEventKeepsOrigin(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	siteA := b.call("POST", base+"/sites", map[string]any{"name": "Old site", "timezone": "UTC"}, 201)["id"].(string)
	siteB := b.call("POST", base+"/sites", map[string]any{"name": "New site", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Moved", "siteId": siteA, "profiles": []any{map[string]string{"id": "main", "name": "Main", "url": "rtsp://127.0.0.1/source"}}}, 201)["id"].(string)
	b.call("POST", base+"/cameras/"+camera+"/site-changes", map[string]any{"revision": 1, "siteId": siteB}, 200)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Old site only", "grants": []app.Grant{
		{Action: "live", Scope: "site", ResourceID: siteA},
		{Action: "playback", Scope: "site", ResourceID: siteA},
	}}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "oldsite", "password": "site-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	c := *b
	client := *b.client
	client.Jar, _ = cookiejar.New(nil)
	c.client = &client
	c.login("oldsite", "site-password")
	c.selectTenant(tenant)
	stream, stop := openEvents(t, &c, tenant, "")
	defer stop()
	nextEvent(t, stream, "ResyncRequired")
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	table := pgx.Identifier{b.options.Schema, "event_outbox"}.Sanitize()
	// An event queued before a move cannot reveal current camera state to its
	// previous site's viewers. Historical archive provenance still belongs there.
	_, err = db.Exec(context.Background(), "INSERT INTO "+table+`(tenant_id,resource_id,camera_id,site_id,revision,kind)
 VALUES($1,$2,$2,$3,1,'CameraStatusChanged'),($1,'old-recording',$2,$3,1,'ArchiveChanged')`, tenant, camera, siteA)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatal("event stream ended")
			}
			if e.Type == "CameraStatusChanged" {
				t.Fatal("current camera state leaked through former site")
			}
			if e.Type == "ArchiveChanged" {
				if e.ResourceID != "old-recording" {
					t.Fatalf("unexpected historical event: %+v", e)
				}
				return
			}
		case <-timer.C:
			t.Fatal("historical site event was not delivered")
		}
	}
}
