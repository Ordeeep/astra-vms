package app_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestDeleteOperatorRevokesAccessAndPreservesIdentity(t *testing.T) {
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Deletion test"}, 201)["id"].(string)
	b.selectTenant(tenant)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "removed", "password": "password", "membership": map[string]any{"tenantId": tenant, "groups": []string{}, "revision": 0}}, 201)["id"].(string)
	other := independentBrowser(t, b, "removed", "password", tenant)
	path := "/admin/operators/" + user
	other.call("DELETE", path, map[string]any{"revision": 1}, 403)
	b.call("DELETE", "/admin/operators/"+admin, map[string]any{"revision": 1}, 422)
	b.call("DELETE", path, map[string]any{"revision": 2}, 412)
	other.call("GET", "/me", nil, 200)
	b.call("DELETE", path, map[string]any{"revision": 1}, 204)
	other.call("GET", "/me", nil, 401)
	other.call("POST", "/auth/login", map[string]any{"username": "removed", "password": "password"}, 401)
	for _, path := range []string{"/admin/operators", "/tenants/" + tenant + "/operators"} {
		for _, item := range b.call("GET", path, nil, 200)["items"].([]any) {
			if item.(map[string]any)["id"] == user {
				t.Fatal("deleted operator still listed")
			}
		}
	}
	b.call("PATCH", path, map[string]any{"active": true, "revision": 2}, 412)
	b.call("PUT", "/tenants/"+tenant+"/operators/"+user, map[string]any{"groups": []string{}, "revision": 0}, 404)
	db, err := pgx.Connect(context.Background(), b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	var retained bool
	if err = db.QueryRow(context.Background(), "SELECT deleted_at IS NOT NULL AND NOT active FROM "+pgx.Identifier{b.options.Schema, "operators"}.Sanitize()+" WHERE id=$1", user).Scan(&retained); err != nil || !retained {
		t.Fatalf("historical identity not retained: %v", err)
	}
}
