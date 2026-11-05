package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

func TestProtectedTenantReductionDoesNotStarveAnotherTenant(t *testing.T) {
	protected := ""
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/archive/reduction-preview":
			w.Write([]byte(`{"limit":4096,"usage":4096,"target":0,"toFree":4096}`))
		case "/archive/reductions":
			var in struct{ Tenant string }
			json.NewDecoder(r.Body).Decode(&in)
			if in.Tenant == protected {
				w.Write([]byte(`{"limit":4096,"usage":4096,"target":0,"toFree":4096,"protected":true}`))
			} else {
				w.Write([]byte(`{"limit":0,"usage":0,"target":0,"toFree":0,"applied":true}`))
			}
		case "/captures", "/archive/catalog":
			if r.Method == "GET" {
				w.Write([]byte("[]"))
			} else {
				w.WriteHeader(204)
			}
		case "/archive/usage":
			w.Write([]byte(`{"recording":0,"export":0}`))
		default:
			w.WriteHeader(204)
		}
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "quota-orchestration-test" })
	b.login("admin", "admin")
	protected = b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Independent quota"}, 201)["id"].(string)
	for _, tenant := range []string{protected, other} {
		b.selectTenant(tenant)
		base := "/tenants/" + tenant
		b.call("PUT", base+"/recording-policy", map[string]any{"quotaBytes": 4096, "exportBytes": 4096, "retentionDays": 10, "exportHours": 24, "revision": 0}, 200)
		p := b.call("POST", base+"/quota-previews", map[string]any{"targetBytes": 0, "revision": 1}, 201)
		b.call("POST", base+"/quota-jobs", map[string]any{"previewId": p["id"], "idempotencyKey": "fair-reduction-" + tenant}, 202)
	}
	deadline := time.Now().Add(6 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		list := b.call("GET", "/tenants/"+other+"/quota-jobs", nil, 200)["items"].([]any)
		if list[0].(map[string]any)["status"] == "completed" {
			completed = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !completed {
		t.Fatal("protected tenant starved independent reduction")
	}
	b.selectTenant(protected)
	status := b.call("GET", "/tenants/"+protected+"/quota-jobs", nil, 200)["items"].([]any)[0].(map[string]any)["status"]
	if status != "waiting" {
		t.Fatalf("protected history not preserved: %v", status)
	}
}

func TestLostReductionResponsePreservesExportProtections(t *testing.T) {
	var responseLost, reconciled atomic.Bool
	var cancelledExports atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive/reduction-preview":
			w.Write([]byte(`{"limit":4096,"usage":4096,"target":0,"toFree":4096}`))
		case "/archive/reductions":
			if responseLost.CompareAndSwap(false, true) {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.Write([]byte(`{"limit":4096,"usage":4096,"target":0,"toFree":4096,"protected":true}`))
		case "/archive/limits":
			if responseLost.Load() {
				reconciled.Store(true)
			}
			w.WriteHeader(204)
		case "/archive/exports":
			if r.Method == "DELETE" && responseLost.Load() {
				cancelledExports.Add(1)
			}
			w.WriteHeader(204)
		case "/captures", "/archive/catalog":
			if r.Method == "GET" {
				w.Write([]byte("[]"))
			} else {
				w.WriteHeader(204)
			}
		case "/archive/usage":
			w.Write([]byte(`{"recording":0,"export":0}`))
		default:
			w.WriteHeader(204)
		}
	}))
	defer worker.Close()
	b := newTestAPI(t, func(o *app.Options) { o.WorkerURL = worker.URL; o.WorkerSecret = "reduction-fault-test" })
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	b.call("PUT", base+"/recording-policy", map[string]any{"quotaBytes": 4096, "exportBytes": 4096, "retentionDays": 10, "exportHours": 24, "revision": 0}, 200)
	p := b.call("POST", base+"/quota-previews", map[string]any{"targetBytes": 0, "revision": 1}, 201)
	b.call("POST", base+"/quota-jobs", map[string]any{"previewId": p["id"], "idempotencyKey": "lost-reduction-response"}, 202)
	deadline := time.Now().Add(6 * time.Second)
	for !reconciled.Load() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !responseLost.Load() || !reconciled.Load() {
		t.Fatal("fault was not reconciled")
	}
	if cancelledExports.Load() != 0 {
		t.Fatal("administrative quota recovery cancelled exports")
	}
}
