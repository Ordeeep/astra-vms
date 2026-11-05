package app_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

func TestAuditIsTenantScopedAndNeverCopiesInternalSecrets(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Restricted tenant"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	group := b.call("POST", base+"/groups", map[string]any{"name": "Auditor", "grants": []app.Grant{{Action: "audit.view", Scope: "tenant", ResourceID: tenant}}}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "auditor", "password": "audit-test-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	table := pgx.Identifier{b.options.Schema, "audit_events"}.Sanitize()
	_, err = db.Exec(context.Background(), "INSERT INTO "+table+"(actor_id,tenant_id,action,resource_id,details) VALUES($1,$2,'test.local','visible', $4),($1,$3,'test.foreign','never-visible',$4)", user, tenant, other, []byte(`{"secret":"do-not-return-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	b.call("POST", "/auth/logout", nil, 204)
	b.login("auditor", "audit-test-password")
	b.selectTenant(tenant)
	list := b.call("GET", base+"/audit", nil, 200)
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "do-not-return-secret") || strings.Contains(string(raw), "never-visible") {
		t.Fatal("audit escaped response boundary")
	}
	if !strings.Contains(string(raw), "visible") {
		t.Fatal("authorized event omitted")
	}
	b.call("GET", "/admin/audit", nil, 403)
	b.call("GET", "/tenants/"+other+"/audit", nil, 404)
	b.call("GET", base+"/audit?cursor=bad", nil, 422)
}

func TestUnresponsiveMediaWorkerDoesNotMasqueradeAsDatabaseFailure(t *testing.T) {
	release := make(chan struct{})
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/config" {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		if r.URL.Path == "/captures" || r.URL.Path == "/archive/catalog" {
			w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(204)
	}))
	defer worker.Close()
	defer close(release)
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "diagnostic-test-worker" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	began := time.Now()
	result := b.call("GET", "/tenants/"+tenant+"/diagnostics", nil, 200)
	if result["workerAvailable"] != false || result["database"] != "available" {
		t.Fatal("media failure attributed to database")
	}
	if time.Since(began) > 2*time.Second {
		t.Fatal("diagnostic did not bound worker wait")
	}
}
