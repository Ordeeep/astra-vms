package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestScopedOperatorNeedsExplicitVideoGrantAndTenantContext(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tid := b.call("POST", "/admin/tenants", map[string]string{"name": "Empresa B"}, 201)["id"].(string)
	b.selectTenant(tid)
	base := "/tenants/" + tid
	user := b.call("POST", "/admin/operators", map[string]any{"username": "viewer", "password": "password", "membership": map[string]any{"tenantId": tid, "groups": []string{}, "revision": 0}}, 201)["id"].(string)
	viewer := independentBrowser(t, b, "viewer", "password", tid)
	caps := viewer.call("GET", base+"/capabilities", nil, 200)
	if caps["live"] != false {
		t.Fatalf("implicit video grant %#v", caps)
	}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Ao vivo", "grants": []map[string]string{{"action": "live", "scope": "tenant", "resourceId": tid}}}, 201)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group["id"].(string)}, "revision": 1}, 200)
	caps = viewer.call("GET", base+"/capabilities", nil, 200)
	if caps["live"] != true || caps["export"] != false {
		t.Fatalf("wrong action union %#v", caps)
	}
	viewer.call("GET", "/tenants/not-this-tenant/capabilities", nil, 404)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{}, "revision": 2}, 200)
	caps = viewer.call("GET", base+"/capabilities", nil, 200)
	if caps["live"] != false {
		t.Fatal("revoked video still allowed")
	}
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group["id"].(string)}, "revision": 2}, 412)
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			raw, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("Concurrent %d", i), "grants": []any{}})
			req, _ := http.NewRequestWithContext(ctx, "POST", b.server+"/api/v1"+base+"/groups", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", b.csrf)
			req.Header.Set("X-VMS-Context", b.view)
			res, err := b.client.Do(req)
			if err == nil {
				res.Body.Close()
				if res.StatusCode != 201 {
					err = fmt.Errorf("concurrent write status %d", res.StatusCode)
				}
			}
			done <- err
		}(i)
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}
