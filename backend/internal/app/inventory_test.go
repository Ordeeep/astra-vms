package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

type browserClient struct {
	t          *testing.T
	client     *http.Client
	server     string
	csrf, view string
	options    app.Options
	close      func()
}

func newTestAPI(t *testing.T, configure ...func(*app.Options)) *browserClient {
	t.Helper()
	dsn := os.Getenv("VMS_TEST_DB")
	if dsn == "" {
		t.Skip("real PostgreSQL required")
	}
	options := app.Options{DatabaseURL: dsn, Schema: fmt.Sprintf("test_%d", time.Now().UnixNano()), DataDir: t.TempDir(), SecureCookies: true}
	createdSchema := options.Schema
	for _, f := range configure {
		f(&options)
	}
	a, err := app.New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(a.Handler())
	s.EnableHTTP2 = true
	s.StartTLS()
	stop := sync.OnceFunc(func() { s.Close(); a.Close() })
	t.Cleanup(func() {
		stop()
		if options.Schema == createdSchema {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			db, err := pgx.Connect(ctx, options.DatabaseURL)
			if err != nil {
				t.Errorf("test namespace cleanup: %v", err)
				return
			}
			defer db.Close(ctx)
			if _, err = db.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{createdSchema}.Sanitize()+" CASCADE"); err != nil {
				t.Errorf("test namespace cleanup: %v", err)
			}
		}
	})
	c := s.Client()
	c.Jar, _ = cookiejar.New(nil)
	return &browserClient{t: t, client: c, server: s.URL, options: options, close: stop}
}
func (b *browserClient) call(method, path string, data any, status int) map[string]any {
	b.t.Helper()
	raw, _ := json.Marshal(data)
	r, _ := http.NewRequest(method, b.server+"/api/v1"+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", b.csrf)
	r.Header.Set("X-VMS-Context", b.view)
	res, err := b.client.Do(r)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ = io.ReadAll(res.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	if res.StatusCode != status {
		b.t.Fatalf("%s %s: expected %d got %d: %s", method, path, status, res.StatusCode, raw)
	}
	return out
}
func (b *browserClient) login(username, password string) string {
	r := b.call("POST", "/auth/login", map[string]string{"username": username, "password": password}, 200)
	b.csrf = r["csrfToken"].(string)
	return r["user"].(map[string]any)["id"].(string)
}
func (b *browserClient) selectTenant(tenant string) {
	b.view = b.call("POST", "/contexts", map[string]string{"tenantId": tenant}, 201)["id"].(string)
}
func TestInventoryVersionsAndScopedGrants(t *testing.T) {
	b := newTestAPI(t)
	user := b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Factory"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	a := b.call("POST", base+"/sites", map[string]any{"name": "Entrada", "timezone": "America/Sao_Paulo", "address": "Rua A"}, 201)
	siteA := a["id"].(string)
	siteB := b.call("POST", base+"/sites", map[string]any{"name": "Fundos", "timezone": "UTC", "address": ""}, 201)["id"].(string)
	input := map[string]any{"name": "Portaria", "siteId": siteA, "profiles": []any{map[string]string{"id": "main", "name": "Principal", "url": "rtsp://camera-user:private-password@127.0.0.1:22554/camera"}}}
	camera := b.call("POST", base+"/cameras", input, 201)
	cid := camera["id"].(string)
	raw, _ := json.Marshal(camera)
	if strings.Contains(string(raw), "private-password") || strings.Contains(string(raw), "rtsp://") {
		t.Fatal("source credentials returned")
	}
	original := camera["sourceVersion"].(string)
	restore := b.options
	restore.DataDir = t.TempDir()
	if restored, e := app.New(context.Background(), restore); e == nil {
		restored.Close()
		t.Fatal("restored database silently replaced its encryption key")
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Site A ao vivo", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: siteA}, {Action: "export", Scope: "site", ResourceID: siteB}}}, 201)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group["id"].(string)}, "revision": 0}, 200)
	current := b.call("GET", base+"/cameras/"+cid, nil, 200)
	if current["capabilities"].(map[string]any)["live"] != true || current["capabilities"].(map[string]any)["export"] != false {
		t.Fatal("cross product grants")
	}
	current = b.call("POST", base+"/cameras/"+cid+"/source-changes", map[string]any{"revision": 1, "profiles": []any{map[string]string{"id": "main", "name": "Nova fonte", "url": "rtsp://127.0.0.1:22554/replacement"}}}, 200)
	if current["id"] != cid || current["sourceVersion"] == original {
		t.Fatal("source replacement lost identity/version")
	}
	current = b.call("POST", base+"/cameras/"+cid+"/site-changes", map[string]any{"revision": 2, "siteId": siteB}, 200)
	if current["siteId"] != siteB || current["capabilities"].(map[string]any)["live"] != false {
		t.Fatal("site scope did not follow current location")
	}
	b.call("POST", base+"/cameras/"+cid+"/site-changes", map[string]any{"revision": 3, "siteId": "foreign-site"}, 404)
	versions := b.call("GET", base+"/cameras/"+cid+"/source-versions", nil, 200)["items"].([]any)
	if len(versions) != 2 {
		t.Fatal("source history lost")
	}
	b.call("PATCH", base+"/sites/"+siteB, map[string]any{"name": "Fundos", "timezone": "UTC", "address": "", "active": false, "revision": 1}, 200)
	current = b.call("GET", base+"/cameras/"+cid, nil, 200)
	if current["active"] != true || current["effectiveActive"] != false {
		t.Fatal("parent deactivation changed child configuration")
	}
	b.call("POST", base+"/groups", map[string]any{"name": "Foreign grant", "grants": []app.Grant{{Action: "live", Scope: "camera", ResourceID: "foreign"}}}, 403)
}
