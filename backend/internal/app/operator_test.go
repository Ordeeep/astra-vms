package app_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestAdminOperatorProfileAndPassword(t *testing.T) {
	b := newTestAPI(t)
	adminID := b.login("admin", "admin")
	created := b.call("POST", "/admin/operators", map[string]any{
		"username": "original", "name": " Original Name ", "email": " original@example.com ", "password": "old-password",
	}, 201)
	userID := created["id"].(string)
	path := "/admin/operators/" + userID
	if created["name"] != "Original Name" || created["email"] != "original@example.com" {
		t.Fatalf("profile not saved: %#v", created)
	}
	other := *b
	other.client = &http.Client{Transport: b.client.Transport}
	other.client.Jar, _ = cookiejar.New(nil)
	other.login("original", "old-password")
	input := map[string]any{"username": "renamed", "name": " Updated Name ", "email": "updated@example.com", "active": true, "revision": 1}
	other.call("PATCH", path, input, 403)
	csrf := b.csrf
	b.csrf = ""
	b.call("PATCH", path, input, 403)
	b.csrf = csrf
	for key, value := range map[string]any{
		"username": " ", "name": "Bad\nName", "email": "invalid-email", "password": strings.Repeat("a", 1025),
	} {
		invalid := map[string]any{}
		for k, v := range input {
			invalid[k] = v
		}
		invalid[key] = value
		b.call("PATCH", path, invalid, 422)
	}
	input["username"] = "ADMIN"
	b.call("PATCH", path, input, 409)
	input["username"] = "renamed"
	b.call("PATCH", path, input, 204)
	b.call("PATCH", path, input, 412)
	other.call("POST", "/auth/logout", nil, 204)
	other.call("POST", "/auth/login", map[string]any{"username": "original", "password": "old-password"}, 401)
	other.login("updated@example.com", "old-password")
	listed := b.call("GET", "/admin/operators", nil, 200)
	var saved map[string]any
	for _, item := range listed["items"].([]any) {
		u := item.(map[string]any)
		if u["id"] == userID {
			saved = u
		}
	}
	if saved == nil || saved["username"] != "renamed" || saved["name"] != "Updated Name" || saved["email"] != "updated@example.com" || saved["revision"] != float64(2) {
		t.Fatalf("list lost profile fields: %#v", saved)
	}
	if _, ok := saved["password"]; ok {
		t.Fatal("password exposed in list")
	}
	b.call("POST", "/admin/operators", map[string]any{"username": "UPDATED@example.com", "password": "another-password"}, 409)
	reserved := b.call("POST", "/admin/operators", map[string]any{"username": "reserved@example.com", "password": "another-password"}, 201)
	input["revision"] = 2
	input["email"] = "RESERVED@example.com"
	b.call("PATCH", path, input, 409)
	input["email"] = "updated@example.com"
	input["username"] = "reserved@example.com"
	b.call("PATCH", path, input, 409)
	input["username"] = "renamed"
	input["password"] = "new-password"
	b.call("PATCH", path, input, 204)
	other.call("GET", "/me", nil, 401)
	other.call("POST", "/auth/login", map[string]any{"username": "renamed", "password": "old-password"}, 401)
	other.login("renamed", "new-password")
	// Blank passwords and legacy status-only updates keep credentials and profile.
	b.call("PATCH", path, map[string]any{"password": "", "active": true, "revision": 3}, 204)
	other.call("GET", "/me", nil, 200)
	other.call("POST", "/auth/logout", nil, 204)
	other.login("renamed", "new-password")
	b.call("PATCH", path, map[string]any{"active": false, "revision": 4}, 204)
	other.call("GET", "/me", nil, 401)
	// An administrator changing their own password retains only their current session.
	second := *b
	second.client = &http.Client{Transport: b.client.Transport}
	second.client.Jar, _ = cookiejar.New(nil)
	second.login("admin", "admin")
	b.call("PATCH", "/admin/operators/"+adminID, map[string]any{"password": "admin-new-password", "active": true, "revision": 1}, 204)
	b.call("GET", "/me", nil, 200)
	second.call("GET", "/me", nil, 401)
	// Audit details must never contain a plaintext password or its hash.
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	var leaked bool
	if err = db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM `+pgx.Identifier{b.options.Schema, "audit_events"}.Sanitize()+` WHERE details::text LIKE '%new-password%' OR details::text LIKE '%old-password%' OR details ? 'password' OR details ? 'password_hash')`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked {
		t.Fatal("credential leaked into audit")
	}
	// Creation applies the same email namespace validation.
	b.call("POST", "/admin/operators", map[string]any{"username": "third", "email": reserved["username"], "password": "third-password"}, 409)
}
