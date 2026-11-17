package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/media"
)

func TestInstallationAdministrationAuthorizesExportsWithoutExplicitMembership(t *testing.T) {
	dsn := os.Getenv("VMS_TEST_DB")
	if dsn == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("test_export_%d", time.Now().UnixNano())
	a, err := New(ctx, Options{DatabaseURL: dsn, Schema: schema, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		db, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Error(err)
			return
		}
		defer db.Close(ctx)
		if _, err := db.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	var actor string
	if err := a.db.QueryRow(ctx, "SELECT id FROM operators WHERE username='admin'").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	tenant := newID()
	if _, err := a.db.Exec(ctx, "INSERT INTO tenants(id,name) VALUES($1,'Future environment')", tenant); err != nil {
		t.Fatal(err)
	}
	plan := media.ExportPlan{Tenant: tenant, Clips: []media.ExportClip{{Index: media.SegmentIndex{Tenant: tenant, Site: "site", Camera: "camera"}}}}
	if !a.exportAuthorized(ctx, actor, tenant, plan) {
		t.Fatal("installation administration could not export without an explicit environment membership")
	}
	if _, err := a.db.Exec(ctx, "UPDATE installation_groups SET grants='[]'"); err != nil {
		t.Fatal(err)
	}
	if a.exportAuthorized(ctx, actor, tenant, plan) {
		t.Fatal("revoked installation administration retained export access")
	}
}
