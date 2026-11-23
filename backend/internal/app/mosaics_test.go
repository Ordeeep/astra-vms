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

func TestMosaicLayoutRoundTripsAndChecksCapacity(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]string{"name": "Layouts"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant + "/mosaics"
	positions := make([]map[string]string, 108)
	for i := range positions {
		positions[i] = map[string]string{"cameraId": "", "profile": ""}
	}
	input := map[string]any{"name": "Dense", "visibility": "personal", "layout": "12x9", "positions": positions}
	mosaic := b.call("POST", base, input, 201)
	url := base + "/" + mosaic["id"].(string)
	if stored := b.call("GET", url, nil, 200); stored["layout"] != "12x9" || len(stored["positions"].([]any)) != 108 {
		t.Fatalf("dense layout was not preserved: %v", stored)
	}
	input["layout"] = "corner-6"
	input["revision"] = 1
	b.call("PUT", url, input, 422)
	input["positions"] = positions[:6]
	b.call("PUT", url, input, 200)
	if stored := b.call("GET", url, nil, 200); stored["layout"] != "corner-6" {
		t.Fatalf("asymmetric layout was not preserved: %v", stored)
	}
	input["layout"] = "unknown"
	b.call("POST", base, input, 422)
}
