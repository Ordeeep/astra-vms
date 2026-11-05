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

func TestLocalLoginContextsPasswordAndLogout(t *testing.T) {
	dsn := os.Getenv("VMS_TEST_DB")
	if dsn == "" {
		t.Skip("VMS_TEST_DB required for real PostgreSQL integration")
	}
	options := app.Options{DatabaseURL: dsn, Schema: fmt.Sprintf("test_%d", time.Now().UnixNano()), SecureCookies: true}
	api, err := app.New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	request := func(method, path string, body any, csrf string) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
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
	status, login := request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "admin"}, "")
	if status != 200 {
		t.Fatalf("login %d %#v", status, login)
	}
	csrf := login["csrfToken"].(string)
	status, me := request("GET", "/api/v1/me", nil, "")
	if status != 200 {
		t.Fatal(me)
	}
	tenants := me["tenants"].([]any)
	if len(tenants) != 1 {
		t.Fatalf("default tenants: %#v", tenants)
	}
	tenant := tenants[0].(map[string]any)["id"].(string)
	status, first := request("POST", "/api/v1/contexts", map[string]string{"tenantId": tenant}, csrf)
	if status != 201 {
		t.Fatal(first)
	}
	_, second := request("POST", "/api/v1/contexts", map[string]string{"tenantId": tenant}, csrf)
	if first["id"] == second["id"] {
		t.Fatal("tabs reused same context")
	}
	status, _ = request("POST", "/api/v1/auth/password", map[string]string{"currentPassword": "admin", "newPassword": "custom-password"}, "")
	if status != 403 {
		t.Fatal("password change without CSRF was accepted")
	}
	status, _ = request("POST", "/api/v1/auth/password", map[string]string{"currentPassword": "admin", "newPassword": "custom-password"}, csrf)
	if status != 204 {
		t.Fatalf("password change %d", status)
	}
	status, _ = request("POST", "/api/v1/auth/logout", nil, csrf)
	if status != 204 {
		t.Fatalf("logout %d", status)
	}
	status, _ = request("GET", "/api/v1/me", nil, "")
	if status != 401 {
		t.Fatal("logout session survived")
	}
	status, _ = request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "admin"}, "")
	if status != 401 {
		t.Fatal("old password accepted")
	}
	// Only one core may control the installation. A real restart must preserve
	// the changed credentials instead of executing bootstrap again.
	if duplicate, err := app.New(context.Background(), options); err == nil {
		duplicate.Close()
		t.Fatal("two cores acquired installation ownership")
	}
	server.Close()
	api.Close()
	reopened, err := app.New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	server = httptest.NewTLSServer(reopened.Handler())
	defer server.Close()
	client = server.Client()
	client.Jar, _ = cookiejar.New(nil)
	status, _ = request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "custom-password"}, "")
	if status != 200 {
		t.Fatalf("persisted password login %d", status)
	}
}
