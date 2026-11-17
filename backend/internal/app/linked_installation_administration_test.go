package app_test

import (
	"testing"

	"vms.local/backend/internal/app"
)

func TestEnvironmentGroupLinksInstallationAdministrationAndRestoresGranularAccess(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Linked administration"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	granular := []app.Grant{{Action: "live", Scope: "tenant", ResourceID: tenant}}
	full := append(append([]app.Grant{}, granular...), app.Grant{Action: "installation.manage", Scope: "installation", ResourceID: "installation"})
	group := b.call("POST", base+"/groups", map[string]any{"name": "Linked", "grants": full}, 201)["id"].(string)
	user := b.call("POST", "/admin/operators", map[string]any{"username": "linked", "password": "password", "membership": map[string]any{"tenantId": tenant, "groups": []string{group}, "revision": 0}}, 201)["id"].(string)
	linked := independentBrowser(t, b, "linked", "password", tenant)
	assertFull := func(base string) {
		t.Helper()
		for action, enabled := range linked.call("GET", base+"/capabilities", nil, 200) {
			if enabled != true {
				t.Fatalf("missing linked capability %s", action)
			}
		}
	}
	assertFull(base)
	linked.call("GET", "/admin/operators", nil, 200)
	// The option covers a future environment without creating copied grants or memberships.
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Created later"}, 201)["id"].(string)
	linked.selectTenant(other)
	assertFull("/tenants/" + other)
	// Turning it off preserves only the independent, explicitly selected access.
	b.call("PUT", base+"/groups/"+group, map[string]any{"name": "Linked", "grants": granular, "revision": 1}, 200)
	linked.call("GET", "/admin/operators", nil, 403)
	linked.call("GET", "/tenants/"+other+"/capabilities", nil, 404)
	linked.selectTenant(tenant)
	caps := linked.call("GET", base+"/capabilities", nil, 200)
	if caps["live"] != true || caps["playback"] != false || caps["permissions.manage"] != false {
		t.Fatalf("granular selection not restored: %#v", caps)
	}
	// Relinking and revoking membership both take effect in an existing session.
	b.call("PUT", base+"/groups/"+group, map[string]any{"name": "Linked", "grants": full, "revision": 2}, 200)
	assertFull(base)
	b.call("PUT", base+"/operators/"+user, map[string]any{"groups": []string{}, "revision": 1}, 200)
	linked.call("GET", "/admin/operators", nil, 403)
	if linked.call("GET", base+"/capabilities", nil, 200)["live"] != false {
		t.Fatal("membership revocation retained copied permissions")
	}
}

func TestLinkedInstallationAdministrationRejectsDelegatedChangesAndProtectsLastAdmin(t *testing.T) {
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Protected administration"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	full := []app.Grant{{Action: "installation.manage", Scope: "installation", ResourceID: "installation"}}
	group := b.call("POST", base+"/groups", map[string]any{"name": "Full", "grants": full}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+admin, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	localGroup := b.call("POST", base+"/groups", map[string]any{"name": "Local manager", "grants": []app.Grant{{Action: "permissions.manage", Scope: "tenant", ResourceID: tenant}}}, 201)["id"].(string)
	localID := b.call("POST", "/admin/operators", map[string]any{"username": "manager", "password": "password", "membership": map[string]any{"tenantId": tenant, "groups": []string{localGroup}, "revision": 0}}, 201)["id"].(string)
	local := independentBrowser(t, b, "manager", "password", tenant)
	local.call("POST", base+"/groups", map[string]any{"name": "Escalation", "grants": full}, 403)
	local.call("PUT", base+"/groups/"+group, map[string]any{"name": "Removed", "grants": []app.Grant{}, "revision": 1}, 403)
	local.call("PUT", base+"/operators/"+localID, map[string]any{"groups": []string{group}, "revision": 1}, 403)
	local.call("PUT", base+"/operators/"+admin, map[string]any{"groups": []string{}, "revision": 1}, 403)
	// The environment group can be the only source of installation administration.
	b.call("PATCH", "/admin/operators/"+admin, map[string]any{"active": true, "installationGroups": []string{}, "revision": 1}, 204)
	b.call("PUT", base+"/groups/"+group, map[string]any{"name": "Removed", "grants": []app.Grant{}, "revision": 1}, 409)
	b.call("PUT", base+"/operators/"+admin, map[string]any{"groups": []string{}, "revision": 1}, 409)
	b.call("PATCH", "/admin/tenants/"+tenant, map[string]any{"name": "Protected administration", "active": false, "revision": 1}, 409)
	b.call("GET", "/admin/operators", nil, 200)
}

func TestInstallationAdministrationCanChangeGroupOriginAtomically(t *testing.T) {
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Atomic origin change"}, 201)["id"].(string)
	b.selectTenant(tenant)
	group := b.call("POST", "/tenants/"+tenant+"/groups", map[string]any{"name": "New administration origin", "grants": []app.Grant{{Action: "installation.manage", Scope: "installation", ResourceID: "installation"}}}, 201)["id"].(string)
	b.call("PATCH", "/admin/operators/"+admin, map[string]any{"active": true, "installationGroups": []string{}, "membership": map[string]any{"tenantId": tenant, "groups": []string{group}, "revision": 0}, "revision": 1}, 204)
	b.call("GET", "/admin/operators", nil, 200)
}
