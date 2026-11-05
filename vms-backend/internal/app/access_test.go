package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"vms.local/backend/internal/app"
)

func TestGlobalAdminNeedsExplicitVideoGrantAndTenantContext(t *testing.T) {
	dsn := os.Getenv("VMS_TEST_DB")
	if dsn == "" {
		t.Skip("VMS_TEST_DB required")
	}
	api, err := app.New(context.Background(), app.Options{DatabaseURL: dsn, Schema: fmt.Sprintf("test_%d", time.Now().UnixNano()), SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	csrf, contextID := "", ""
	call := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("X-VMS-Context", contextID)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ = io.ReadAll(res.Body)
		var result map[string]any
		json.Unmarshal(raw, &result)
		return res.StatusCode, result
	}
	status, login := call("POST", "/auth/login", map[string]string{"username": "admin", "password": "admin"})
	if status != 200 {
		t.Fatal(login)
	}
	csrf = login["csrfToken"].(string)
	user := login["user"].(map[string]any)["id"].(string)
	status, tenant := call("POST", "/admin/tenants", map[string]string{"name": "Empresa B"})
	if status != 201 {
		t.Fatalf("tenant %d %#v", status, tenant)
	}
	tid := tenant["id"].(string)
	base := "/tenants/" + tid
	_, ctx := call("POST", "/contexts", map[string]string{"tenantId": tid})
	contextID = ctx["id"].(string)
	status, caps := call("GET", base+"/capabilities", nil)
	if status != 200 || caps["live"] != false {
		t.Fatalf("implicit video grant %d %#v", status, caps)
	}
	status, group := call("POST", base+"/groups", map[string]any{"name": "Ao vivo", "grants": []map[string]string{{"action": "live", "scope": "tenant", "resourceId": tid}}})
	if status != 201 {
		t.Fatalf("group %d %#v", status, group)
	}
	status, result := call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group["id"].(string)}})
	if status != 200 {
		t.Fatalf("membership %d %#v", status, result)
	}
	_, caps = call("GET", base+"/capabilities", nil)
	if caps["live"] != true || caps["export"] != false {
		t.Fatalf("wrong action union %#v", caps)
	}
	status, _ = call("GET", "/tenants/not-this-tenant/capabilities", nil)
	if status != 404 {
		t.Fatalf("context crossed tenant %d", status)
	}
	status, _ = call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{}, "revision": 1})
	if status != 200 {
		t.Fatal(status)
	}
	_, caps = call("GET", base+"/capabilities", nil)
	if caps["live"] != false {
		t.Fatal("revoked video still allowed")
	}
	status, _ = call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{group["id"].(string)}, "revision": 1})
	if status != 412 {
		t.Fatalf("stale membership edit accepted: %d", status)
	}
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			raw, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("Concurrent %d", i), "grants": []any{}})
			req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1"+base+"/groups", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", csrf)
			req.Header.Set("X-VMS-Context", contextID)
			res, err := client.Do(req)
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
