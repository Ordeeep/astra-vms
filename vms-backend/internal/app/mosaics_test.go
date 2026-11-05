package app_test

import "testing"

func TestMosaicsRemainTenantScopedAndUseRevision(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	t1 := b.call("POST", "/admin/tenants", map[string]string{"name": "A"}, 201)["id"].(string)
	t2 := b.call("POST", "/admin/tenants", map[string]string{"name": "B"}, 201)["id"].(string)
	b.selectTenant(t1)
	base := "/tenants/" + t1
	site := b.call("POST", base+"/sites", map[string]string{"name": "Site", "address": "", "timezone": "UTC"}, 201)["id"].(string)
	cam := b.call("POST", base+"/cameras", map[string]any{"name": "Cam", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://127.0.0.1/test"}}}, 201)["id"].(string)
	mosaic := b.call("POST", base+"/mosaics", map[string]any{"name": "Minha vista", "visibility": "personal", "positions": []map[string]any{{"cameraId": cam, "profile": ""}}}, 201)
	id := mosaic["id"].(string)
	b.call("PUT", base+"/mosaics/"+id, map[string]any{"name": "Vista editada", "visibility": "personal", "positions": []map[string]any{{"cameraId": cam, "profile": "main"}}, "revision": 1}, 200)
	b.call("PUT", base+"/mosaics/"+id, map[string]any{"name": "Edição antiga", "visibility": "personal", "positions": []any{}, "revision": 1}, 412)
	b.selectTenant(t2)
	base2 := "/tenants/" + t2
	b.call("GET", base2+"/mosaics/"+id, nil, 404)
	b.call("POST", base2+"/mosaics", map[string]any{"name": "Câmera estrangeira", "visibility": "personal", "positions": []map[string]any{{"cameraId": cam}}}, 404)
}
