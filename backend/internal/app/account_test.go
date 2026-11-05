package app_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/app"
)

func TestOwnProfileValidationIsolationAndPersistence(t *testing.T) {
	b := newTestAPI(t)
	adminID := b.login("admin", "admin")
	created := b.call("POST", "/admin/operators", map[string]any{"username": "operator", "password": "test-password"}, 201)
	other := *b
	other.client = &http.Client{Transport: b.client.Transport}
	other.client.Jar, _ = cookiejar.New(nil)
	other.call("PATCH", "/me", map[string]any{"name": "Anonymous", "email": "anon@example.com", "revision": 1}, 401)
	otherID := other.login("operator", "test-password")
	if otherID != created["id"] || otherID == adminID {
		t.Fatal("operator identity mismatch")
	}
	profile := map[string]any{"name": "  Pedro Silva  ", "email": "  Pedro@example.com  ", "revision": 1, "currentPassword": "test-password"}
	csrf := other.csrf
	other.csrf = ""
	other.call("PATCH", "/me", profile, 403)
	other.csrf = csrf
	for _, input := range []map[string]any{
		{"name": " ", "email": "pedro@example.com", "revision": 1},
		{"name": strings.Repeat("á", 129), "email": "pedro@example.com", "revision": 1},
		{"name": "Bad\nName", "email": "pedro@example.com", "revision": 1},
		{"name": "Pedro", "email": "invalid", "revision": 1},
		{"name": "Pedro", "email": "Pedro <pedro@example.com>", "revision": 1},
		{"name": "Pedro", "email": strings.Repeat("x", 250) + "@example.com", "revision": 1},
	} {
		other.call("PATCH", "/me", input, 422)
	}
	other.call("PATCH", "/me", map[string]any{"name": "Pedro", "email": "pedro@example.com", "revision": 1, "globalAdmin": true}, 400)
	other.call("PATCH", "/me", map[string]any{"name": "Pedro", "email": "pedro@example.com", "revision": 1, "id": adminID}, 400)
	updated := other.call("PATCH", "/me", profile, 200)
	if updated["id"] != otherID || updated["name"] != "Pedro Silva" || updated["email"] != "Pedro@example.com" || updated["username"] != "operator" || updated["globalAdmin"] != false || updated["revision"] != float64(2) {
		t.Fatalf("unexpected saved profile: %#v", updated)
	}
	other.call("PATCH", "/me", profile, 412)
	admin := b.call("GET", "/me", nil, 200)["user"].(map[string]any)
	if admin["name"] != "admin" || admin["email"] != "" {
		t.Fatal("self-service changed another operator")
	}
	// A normal operator with no tenant membership can manage their own account.
	me := other.call("GET", "/me", nil, 200)
	if len(me["tenants"].([]any)) != 0 || me["user"].(map[string]any)["email"] != updated["email"] {
		t.Fatal("profile unexpectedly depends on tenant access")
	}
	other.call("POST", "/auth/password", map[string]any{"currentPassword": "wrong", "newPassword": "new-password"}, 403)
	secondSession := other
	secondSession.client = &http.Client{Transport: b.client.Transport}
	secondSession.client.Jar, _ = cookiejar.New(nil)
	secondSession.login("operator", "test-password")
	other.call("POST", "/auth/password", map[string]any{"currentPassword": "test-password", "newPassword": "new-password"}, 204)
	secondSession.call("GET", "/me", nil, 401)
	other.call("GET", "/me", nil, 200)
	other.call("POST", "/auth/logout", nil, 204)
	other.call("POST", "/auth/login", map[string]any{"username": "operator", "password": "test-password"}, 401)
	other.login("operator", "new-password")
	b.close()
	restarted := newTestAPI(t, func(o *app.Options) { *o = b.options })
	restarted.login("operator", "new-password")
	persisted := restarted.call("GET", "/me", nil, 200)["user"].(map[string]any)
	if persisted["name"] != updated["name"] || persisted["email"] != updated["email"] {
		t.Fatal("profile did not survive restart")
	}
}

func TestProfileMigrationPreservesExistingCredentials(t *testing.T) {
	b := newTestAPI(t)
	userID := b.login("admin", "admin")
	b.call("POST", "/auth/password", map[string]any{"currentPassword": "admin", "newPassword": "preserved-password"}, 204)
	b.close()
	ctx := context.Background()
	db, err := pgx.Connect(ctx, b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	// Recreate the version-one shape only in this test's temporary namespace.
	schema := pgx.Identifier{b.options.Schema}.Sanitize()
	if _, err = db.Exec(ctx, "DROP TABLE "+schema+".camera_deletions; DROP FUNCTION "+schema+".require_operational_camera() CASCADE; ALTER TABLE "+schema+".cameras DROP COLUMN deleted_at"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "ALTER TABLE "+schema+".operators DROP COLUMN name,DROP COLUMN email; ALTER TABLE "+schema+".camera_sources DROP COLUMN connection_type,DROP COLUMN connection,DROP COLUMN connection_revision; DELETE FROM "+schema+".schema_migrations WHERE version>=2"); err != nil {
		t.Fatal(err)
	}
	restarted := newTestAPI(t, func(o *app.Options) { *o = b.options })
	if restarted.login("admin", "preserved-password") != userID {
		t.Fatal("migration changed the existing identity")
	}
	me := restarted.call("GET", "/me", nil, 200)
	user := me["user"].(map[string]any)
	if user["name"] != "admin" || user["email"] != "" || user["globalAdmin"] != true || len(me["tenants"].([]any)) != 1 {
		t.Fatal("migration did not preserve existing access")
	}
}
