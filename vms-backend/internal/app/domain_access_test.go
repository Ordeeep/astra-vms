package app_test

import (
	"net/http/cookiejar"
	"testing"

	"vms.local/backend/internal/app"
)

func independentBrowser(t *testing.T, source *browserClient, username, password, tenant string) *browserClient {
	t.Helper()
	b := *source
	client := *source.client
	client.Jar, _ = cookiejar.New(nil)
	b.client = &client
	b.login(username, password)
	b.selectTenant(tenant)
	return &b
}

func TestDelegatedGrantsSurviveGrantorRevocationWithoutAllowingNewEscalation(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]any{"name": "Delegated site", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "First", "siteId": site, "profiles": []any{map[string]string{"id": "main", "name": "Main", "url": "rtsp://127.0.0.1/camera"}}}, 201)["id"].(string)
	manager := b.call("POST", "/admin/operators", map[string]any{"username": "manager", "password": "manager-password"}, 201)["id"].(string)
	operator := b.call("POST", "/admin/operators", map[string]any{"username": "viewer", "password": "viewer-password"}, 201)["id"].(string)
	managerGroup := b.call("POST", base+"/groups", map[string]any{"name": "Manager powers", "grants": []app.Grant{
		{Action: "permissions.manage", Scope: "tenant", ResourceID: tenant},
		{Action: "live", Scope: "site", ResourceID: site},
	}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+manager, map[string]any{"groups": []string{managerGroup}, "revision": 0}, 200)
	// A tenant manager can operate on existing tenant membership, without
	// discovering or creating global identities belonging to another company.
	b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{}, "revision": 0}, 200)
	m := independentBrowser(t, b, "manager", "manager-password", tenant)
	delegated := m.call("POST", base+"/groups", map[string]any{"name": "Site viewers", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}}, 201)["id"].(string)
	m.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{delegated}, "revision": 1}, 200)
	m.call("POST", base+"/groups", map[string]any{"name": "Too broad", "grants": []app.Grant{{Action: "live", Scope: "tenant", ResourceID: tenant}}}, 403)
	m.call("POST", base+"/groups", map[string]any{"name": "Wrong action", "grants": []app.Grant{{Action: "export", Scope: "site", ResourceID: site}}}, 403)
	viewer := independentBrowser(t, b, "viewer", "viewer-password", tenant)
	if viewer.call("GET", base+"/cameras/"+camera, nil, 200)["capabilities"].(map[string]any)["live"] != true {
		t.Fatal("delegated site grant not effective")
	}
	newCamera := b.call("POST", base+"/cameras", map[string]any{"name": "Added later", "siteId": site, "profiles": []any{map[string]string{"id": "main", "name": "Main", "url": "rtsp://127.0.0.1/new"}}}, 201)["id"].(string)
	if viewer.call("GET", base+"/cameras/"+newCamera, nil, 200)["capabilities"].(map[string]any)["live"] != true {
		t.Fatal("site grant did not include later camera")
	}
	b.call("PUT", base+"/operators/"+manager, map[string]any{"groups": []string{}, "revision": 1}, 200)
	if viewer.call("GET", base+"/cameras/"+camera, nil, 200)["capabilities"].(map[string]any)["live"] != true {
		t.Fatal("grantor revocation implicitly revoked recipient")
	}
	m.call("POST", base+"/groups", map[string]any{"name": "After revocation", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}}, 403)
}

func TestInactiveMosaicCreatorPreservesSharedCompositionAndPersonalPrivacy(t *testing.T) {
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	creator := b.call("POST", "/admin/operators", map[string]any{"username": "creator", "password": "creator-password"}, 201)["id"].(string)
	group := b.call("POST", base+"/groups", map[string]any{"name": "Compositions", "grants": []app.Grant{{Action: "mosaics.manage", Scope: "tenant", ResourceID: tenant}}}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+creator, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	c := independentBrowser(t, b, "creator", "creator-password", tenant)
	shared := c.call("POST", base+"/mosaics", map[string]any{"name": "Shared", "visibility": "shared", "positions": []any{}}, 201)["id"].(string)
	personal := c.call("POST", base+"/mosaics", map[string]any{"name": "Private", "visibility": "personal", "positions": []any{}}, 201)["id"].(string)
	b.call("PATCH", "/admin/operators/"+creator, map[string]any{"active": false, "globalAdmin": false, "revision": 1}, 204)
	item := b.call("GET", base+"/mosaics/"+shared, nil, 200)
	if item["ownerId"] != creator || item["editable"] != true {
		t.Fatal("shared composition lost ownership or management")
	}
	b.call("GET", base+"/mosaics/"+personal, nil, 404)
	b.call("PUT", base+"/mosaics/"+shared, map[string]any{"name": "Maintained", "visibility": "shared", "positions": []any{}, "revision": 1}, 200)
	b.call("PUT", base+"/mosaics/"+shared, map[string]any{"name": "Privatized", "visibility": "personal", "positions": []any{}, "revision": 2}, 403)
	// Managing a shared composition never adds video permissions.
	b.call("PUT", base+"/operators/"+admin, map[string]any{"groups": []string{group}, "revision": 1}, 200)
	if b.call("GET", base+"/capabilities", nil, 200)["live"] != false {
		t.Fatal("composition management implicitly granted live video")
	}
}
