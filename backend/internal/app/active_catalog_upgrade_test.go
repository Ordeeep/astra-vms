package app_test

import (
	"context"
	"testing"
	"vms.local/backend/internal/app"
)

func TestActiveCatalogUpgradesExistingInstallation(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]string{"name": "Upgrade", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Upgrade camera", "siteId": site, "profiles": []map[string]string{{"id": "main", "name": "Principal", "url": "rtsp://127.0.0.1/upgrade-fixture"}}}, 201)["id"].(string)
	db := cameraDeletionDB(t, b)
	key := cameraTestKey(t, db, tenant, camera)
	b.close()
	rewindCameraNumbers(t, db)
	// Version 8 is the installation that predates published checkpoints.
	if _, err := db.Exec(context.Background(), `ALTER TABLE archive_segments DROP COLUMN catalog_sequence; ALTER TABLE mosaics DROP COLUMN layout; DELETE FROM schema_migrations WHERE version>8;`); err != nil {
		t.Fatal(err)
	}
	upgraded, err := app.New(context.Background(), b.options)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var exists bool
	if err = db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='archive_segments' AND column_name='catalog_sequence')`).Scan(&exists); err != nil || !exists {
		t.Fatal("existing installation did not receive checkpoint migration", err)
	}
	_, err = db.Exec(context.Background(), `INSERT INTO archive_segments(id,tenant_id,camera_id,site_id,source_version,profile_id,start_at,end_at,bytes,charged,metadata,catalog_sequence)
 VALUES('upgrade-object',$1,$2,$3,'source','main','2026-11-19T12:00:00Z','2026-11-19T12:00:01Z',100,4096,'{"parts":[]}',33);
 `, tenant, key, site)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(context.Background(), `UPDATE archive_segments SET end_at='2026-11-19T12:00:02Z',metadata='{"parts":[{"start":"2026-11-19T12:00:00Z","end":"2026-11-19T12:00:02Z"}]}',catalog_sequence=34 WHERE id='upgrade-object'`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(context.Background(), `SELECT count(*) FROM event_outbox WHERE resource_id='upgrade-object' AND kind='ArchiveChanged' AND camera_id=$1 AND payload ? 'start' AND payload ? 'end' AND revision IN (33,34)`, key).Scan(&count); err != nil || count != 2 {
		t.Fatal("upgrade did not replace the archive event contract", count, err)
	}
}
