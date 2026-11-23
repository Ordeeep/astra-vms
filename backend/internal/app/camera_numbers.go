package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const cameraNumbersMigration = `
 ALTER TABLE tenants ADD COLUMN last_camera_number bigint NOT NULL DEFAULT 0;
 ALTER TABLE cameras ADD COLUMN camera_number bigint;
 ALTER TABLE cameras DISABLE TRIGGER camera_event;
 WITH first_locations AS (
  SELECT tenant_id,camera_id,min(id) AS first_location FROM camera_locations GROUP BY tenant_id,camera_id
 ), first_sources AS (
  SELECT tenant_id,camera_id,min(created_at) AS first_source FROM camera_sources GROUP BY tenant_id,camera_id
 ), numbered AS (
  SELECT c.id,row_number() OVER(PARTITION BY c.tenant_id ORDER BY l.first_location NULLS LAST,s.first_source NULLS LAST,c.id) AS number
  FROM cameras c
  LEFT JOIN first_locations l ON l.tenant_id=c.tenant_id AND l.camera_id=c.id
  LEFT JOIN first_sources s ON s.tenant_id=c.tenant_id AND s.camera_id=c.id
  WHERE c.deleted_at IS NULL
 )
 UPDATE cameras c SET camera_number=n.number FROM numbered n WHERE c.id=n.id;
 ALTER TABLE cameras ENABLE TRIGGER camera_event;
 UPDATE tenants t SET last_camera_number=n.last_number FROM (
  SELECT tenant_id,max(camera_number) AS last_number FROM cameras WHERE camera_number IS NOT NULL GROUP BY tenant_id
 ) n WHERE t.id=n.tenant_id;
 ALTER TABLE cameras ADD CONSTRAINT camera_number_per_tenant UNIQUE(tenant_id,camera_number);
 ALTER TABLE cameras ADD CONSTRAINT camera_number_valid CHECK (
  (camera_number IS NOT NULL AND camera_number>0) OR (camera_number IS NULL AND deleted_at IS NOT NULL)
 );
 CREATE FUNCTION assign_camera_number() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  UPDATE tenants SET last_camera_number=last_camera_number+1 WHERE id=NEW.tenant_id RETURNING last_camera_number INTO NEW.camera_number;
  RETURN NEW;
 END $$;
 CREATE TRIGGER assign_camera_number BEFORE INSERT ON cameras FOR EACH ROW EXECUTE FUNCTION assign_camera_number();
 CREATE FUNCTION public_camera_grants(owner_tenant text, grants jsonb) RETURNS jsonb LANGUAGE sql STABLE AS $$
  SELECT coalesce(jsonb_agg(CASE
   WHEN c.camera_number IS NULL THEN g.entry
   WHEN owner_tenant IS NULL THEN g.entry || jsonb_build_object('resourceId',c.camera_number::text,'tenantId',c.tenant_id)
   ELSE g.entry || jsonb_build_object('resourceId',c.camera_number::text)
  END ORDER BY g.ordinal),'[]'::jsonb)
  FROM jsonb_array_elements(grants) WITH ORDINALITY g(entry,ordinal)
  LEFT JOIN cameras c ON g.entry->>'scope'='camera' AND c.id=g.entry->>'resourceId'
   AND (owner_tenant IS NULL OR c.tenant_id=owner_tenant)
 $$;
 `

func cameraNumber(ref string) int64 {
	n, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != ref {
		return 0
	}
	return n
}

// resolveCameraKey accepts the public number and the original installation key.
// Handlers apply their usual tenant and permission checks after resolution.
func resolveCameraKey(ctx context.Context, db permissionReader, tenant, ref string) (string, error) {
	number := cameraNumber(ref)
	if number == 0 {
		return ref, nil
	}
	var key string
	err := db.QueryRow(ctx, `SELECT id FROM cameras WHERE tenant_id=$1 AND camera_number=$2`, tenant, number).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, nil
	}
	return key, err
}

func (a *App) requestCameraKey(w http.ResponseWriter, r *http.Request, tenant, ref string) (string, bool) {
	key, err := resolveCameraKey(r.Context(), a.db, tenant, ref)
	if err != nil {
		dbFailure(w)
		return "", false
	}
	return key, true
}

func (a *App) cameraCursor(w http.ResponseWriter, r *http.Request, tenant string) (int64, bool) {
	ref := r.URL.Query().Get("cursor")
	if ref == "" {
		return 0, true
	}
	if number := cameraNumber(ref); number != 0 {
		return number, true
	}
	var number int64
	err := a.db.QueryRow(r.Context(), `SELECT coalesce(camera_number,0) FROM cameras WHERE tenant_id=$1 AND id=$2`, tenant, ref).Scan(&number)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 422, "invalid_cursor")
		return 0, false
	}
	if err != nil {
		dbFailure(w)
		return 0, false
	}
	return number, true
}

// Installation grants need both parts of the public reference. Locally scoped
// grants inherit the tenant from their group. Stored grants keep the unique key.
func resolveCameraGrant(ctx context.Context, db permissionReader, ownerTenant string, grant Grant) (string, Grant, Grant, error) {
	tenant := ownerTenant
	if tenant == "" {
		tenant = grant.TenantID
	} else if grant.TenantID != "" && grant.TenantID != ownerTenant {
		return "", Grant{}, Grant{}, errors.New("foreign camera tenant")
	}
	var key, number string
	err := db.QueryRow(ctx, `SELECT tenant_id,id,coalesce(camera_number::text,id) FROM cameras
 WHERE (tenant_id=$1 AND camera_number=$3) OR (id=$2 AND ($1='' OR tenant_id=$1))`, tenant, grant.ResourceID, cameraNumber(grant.ResourceID)).Scan(&tenant, &key, &number)
	if err != nil {
		return "", Grant{}, Grant{}, err
	}
	stored, public := grant, grant
	stored.ResourceID, stored.TenantID = key, ""
	public.ResourceID, public.TenantID = number, ""
	if ownerTenant == "" && number != key {
		public.TenantID = tenant
	}
	return tenant, stored, public, nil
}
