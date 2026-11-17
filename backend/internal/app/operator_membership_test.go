package app_test

import "testing"

func TestOperatorProfileAndGroupsAreAtomic(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "Operator groups"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	group := b.call("POST", base+"/groups", map[string]any{"name": "Viewers", "grants": []any{}}, 201)["id"].(string)
	membership := map[string]any{"tenantId": tenant, "groups": []string{group}, "revision": 0}
	create := map[string]any{"username": "combined", "password": "password", "membership": membership}
	// An invalid group must roll back the new account, allowing the same username on retry.
	membership["groups"] = []string{"missing"}
	b.call("POST", "/admin/operators", create, 404)
	membership["groups"] = []string{group}
	u := b.call("POST", "/admin/operators", create, 201)["id"].(string)
	assertGroups := func(want int) {
		t.Helper()
		for _, value := range b.call("GET", base+"/operators", nil, 200)["items"].([]any) {
			m := value.(map[string]any)
			if m["id"] == u {
				if len(m["groups"].([]any)) != want {
					t.Fatalf("unexpected groups: %#v", m)
				}
				return
			}
		}
		t.Fatal("membership missing")
	}
	assertGroups(1)
	update := map[string]any{"name": "Changed", "active": true, "revision": 1, "membership": membership}
	// Membership revision is now 1; a stale revision also rolls back the profile revision.
	b.call("PATCH", "/admin/operators/"+u, update, 412)
	membership["revision"] = 1
	membership["groups"] = []string{"missing"}
	b.call("PATCH", "/admin/operators/"+u, update, 404)
	membership["groups"] = []string{}
	b.call("PATCH", "/admin/operators/"+u, update, 204)
	assertGroups(0)
	// Legacy account-only changes preserve memberships.
	b.call("PUT", base+"/operators/"+u, map[string]any{"groups": []string{group}, "revision": 2}, 200)
	b.call("PATCH", "/admin/operators/"+u, map[string]any{"name": "Account only", "active": true, "revision": 2}, 204)
	assertGroups(1)
	// Groups from another environment cannot be assigned here.
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Other environment"}, 201)["id"].(string)
	membership["tenantId"] = other
	membership["revision"] = 0
	membership["groups"] = []string{group}
	update["revision"] = 3
	b.call("PATCH", "/admin/operators/"+u, update, 404)
	assertGroups(1)
}
