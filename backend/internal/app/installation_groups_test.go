package app_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"vms.local/backend/internal/app"
)

func TestInstallationGroupsControlAdministrationAndLastAdmin(t *testing.T) {
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	grant := []app.Grant{{Action: "installation.manage", Scope: "installation", ResourceID: "installation"}}
	group := b.call("POST", "/admin/installation-groups", map[string]any{"name": "Administradores adicionais", "grants": grant}, 201)["id"].(string)
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Environment"}, 201)["id"].(string)
	b.selectTenant(tenant)
	// Environment administrators never acquire installation privileges.
	localGroup := b.call("POST", "/tenants/"+tenant+"/groups", map[string]any{"name": "Gestores locais", "grants": []app.Grant{{Action: "permissions.manage", Scope: "tenant", ResourceID: tenant}}}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "local", "password": "password", "membership": map[string]any{"tenantId": tenant, "groups": []string{localGroup}, "revision": 0}}, 201)["id"].(string)
	local := independentBrowser(t, b, "local", "password", tenant)
	local.call("GET", "/admin/installation-groups", nil, 403)
	local.call("POST", "/admin/installation-groups", map[string]any{"name": "Escalation", "grants": grant}, 403)
	local.call("POST", "/tenants/"+tenant+"/groups", map[string]any{"name": "Escalation", "grants": grant}, 403)
	// The legacy account field can no longer elevate or demote an operator.
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "globalAdmin": true, "revision": 1}, 400)
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "installationGroups": []string{localGroup}, "revision": 1}, 404)
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "installationGroups": []string{group}, "revision": 1}, 204)
	local.call("GET", "/admin/operators", nil, 200)
	// Installation administration links every operation, including video.
	for action, enabled := range local.call("GET", "/tenants/"+tenant+"/capabilities", nil, 200) {
		if enabled != true {
			t.Fatalf("installation administration did not grant %s", action)
		}
	}
	// Updating the group's permissions changes all linked operators immediately.
	b.call("PUT", "/admin/installation-groups/"+group, map[string]any{"name": "Sem administração", "grants": []app.Grant{}, "revision": 1}, 200)
	local.call("GET", "/admin/operators", nil, 403)
	b.call("PUT", "/admin/installation-groups/"+group, map[string]any{"name": "Administradores adicionais", "grants": grant, "revision": 2}, 200)
	local.call("GET", "/admin/operators", nil, 200)
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "installationGroups": []string{}, "revision": 2}, 204)
	local.call("GET", "/admin/operators", nil, 403)
	// Every way of losing the final administrator must roll back.
	for _, patch := range []map[string]any{
		{"active": true, "installationGroups": []string{}, "revision": 1},
		{"active": false, "revision": 1},
	} {
		b.call("PATCH", "/admin/operators/"+admin, patch, 409)
	}
	b.call("PUT", "/admin/installation-groups/installation-administrators", map[string]any{"name": "Sem acesso", "grants": []app.Grant{}, "revision": 1}, 409)
	b.call("GET", "/admin/operators", nil, 200)
	// A stale account update cannot overwrite a concurrent installation membership change.
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "installationGroups": []string{group}, "revision": 2}, 412)
	// Operator removal also removes installation memberships.
	b.call("PATCH", "/admin/operators/"+user, map[string]any{"active": true, "installationGroups": []string{group}, "revision": 3}, 204)
	b.call("DELETE", "/admin/operators/"+user, map[string]any{"revision": 4}, 204)
	local.call("GET", "/me", nil, 401)
}

func TestInstallationGroupsMigrateExistingAdministrators(t *testing.T) {
	b := newTestAPI(t)
	user := b.login("admin", "admin")
	b.close()
	ctx := context.Background()
	db, err := pgx.Connect(ctx, b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	schema := pgx.Identifier{b.options.Schema}.Sanitize()
	// Recreate the previous released schema with its existing administrator flag.
	rewindCameraNumbers(t, cameraDeletionDB(t, b))
	_, err = db.Exec(ctx, "DROP FUNCTION "+schema+".is_installation_admin(text); DROP TABLE "+schema+".installation_group_members; DROP TABLE "+schema+".installation_groups; ALTER TABLE "+schema+".operators ADD COLUMN global_admin boolean NOT NULL DEFAULT false; UPDATE "+schema+".operators SET global_admin=true; ALTER TABLE "+schema+".mosaics DROP COLUMN layout; DELETE FROM "+schema+".schema_migrations WHERE version>=7")
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTestAPI(t, func(o *app.Options) { *o = b.options })
	if restarted.login("admin", "admin") != user {
		t.Fatal("migration changed administrator identity")
	}
	listed := restarted.call("GET", "/admin/operators", nil, 200)
	for _, item := range listed["items"].([]any) {
		u := item.(map[string]any)
		if u["id"] == user {
			if u["globalAdmin"] != true || len(u["installationGroups"].([]any)) != 1 {
				t.Fatalf("administrator was not migrated: %#v", u)
			}
			return
		}
	}
	t.Fatal("administrator missing")
}
