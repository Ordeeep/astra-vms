package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const installationGroupsMigration = `
CREATE TABLE installation_groups (
 id text PRIMARY KEY, name text NOT NULL, grants jsonb NOT NULL DEFAULT '[]', revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE installation_group_members (
 group_id text NOT NULL REFERENCES installation_groups(id), operator_id text NOT NULL REFERENCES operators(id),
 PRIMARY KEY(group_id,operator_id)
);
CREATE INDEX installation_members_operator ON installation_group_members(operator_id);
INSERT INTO installation_groups(id,name,grants) VALUES('installation-administrators','Administradores da instalação',
 '[{"action":"installation.manage","scope":"installation","resourceId":"installation"}]');
INSERT INTO installation_group_members(group_id,operator_id)
 SELECT 'installation-administrators',id FROM operators WHERE global_admin AND deleted_at IS NULL;
ALTER TABLE operators DROP COLUMN global_admin;
CREATE FUNCTION is_installation_admin(target_operator text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM installation_group_members m JOIN installation_groups g ON g.id=m.group_id
 JOIN operators u ON u.id=m.operator_id WHERE m.operator_id=target_operator AND u.active AND u.deleted_at IS NULL
 AND g.grants @> '[{"action":"installation.manage","scope":"installation","resourceId":"installation"}]'::jsonb)
$$;
`

const linkedInstallationAdministrationMigration = `
CREATE OR REPLACE FUNCTION is_installation_admin(target_operator text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM operators u WHERE u.id=target_operator AND u.active AND u.deleted_at IS NULL AND (
  EXISTS(SELECT 1 FROM installation_group_members m JOIN installation_groups g ON g.id=m.group_id
   WHERE m.operator_id=u.id AND g.grants @> '[{"action":"installation.manage","scope":"installation","resourceId":"installation"}]'::jsonb)
  OR EXISTS(SELECT 1 FROM group_members m JOIN permission_groups g ON g.id=m.group_id AND g.tenant_id=m.tenant_id
   JOIN tenants t ON t.id=m.tenant_id WHERE m.operator_id=u.id AND t.active
   AND g.grants @> '[{"action":"installation.manage","scope":"installation","resourceId":"installation"}]'::jsonb)
 ))
$$;
`

func installationAdministration(g Grant) bool {
	return g.Action == "installation.manage" && g.Scope == "installation" && g.ResourceID == "installation"
}

func hasInstallationAdministration(grants []Grant) bool {
	for _, g := range grants {
		if installationAdministration(g) {
			return true
		}
	}
	return false
}

// The access transaction serializes every change that could remove the final administrator.
func requireInstallationAdmin(ctx context.Context, tx pgx.Tx, w http.ResponseWriter) bool {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operators WHERE is_installation_admin(id))").Scan(&exists); err != nil {
		dbFailure(w)
		return false
	}
	if !exists {
		problem(w, 409, "last_installation_admin")
		return false
	}
	return true
}

func saveInstallationMembership(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, operator string, groups []string) bool {
	if len(groups) > 100 {
		problem(w, 422, "too_many_groups")
		return false
	}
	for _, group := range groups {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM installation_groups WHERE id=$1)", group).Scan(&exists); err != nil {
			dbFailure(w)
			return false
		}
		if !exists {
			problem(w, 404, "installation_group_not_found")
			return false
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM installation_group_members WHERE operator_id=$1", operator); err != nil {
		dbFailure(w)
		return false
	}
	for _, group := range groups {
		if _, err := tx.Exec(ctx, "INSERT INTO installation_group_members(group_id,operator_id) VALUES($1,$2) ON CONFLICT DO NOTHING", group, operator); err != nil {
			dbFailure(w)
			return false
		}
	}
	return true
}

func (a *App) installationGroups(w http.ResponseWriter, r *http.Request, id identity) {
	if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		rows, err := a.db.Query(r.Context(), "SELECT id,name,grants,revision FROM installation_groups WHERE id>$1 ORDER BY id LIMIT 201", r.URL.Query().Get("cursor"))
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		items := []Group{}
		for rows.Next() {
			var g Group
			var raw []byte
			if rows.Scan(&g.ID, &g.Name, &raw, &g.Revision) != nil || json.Unmarshal(raw, &g.Grants) != nil {
				dbFailure(w)
				return
			}
			items = append(items, g)
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		cursor := ""
		if len(items) > 200 {
			cursor = items[199].ID
			items = items[:200]
		}
		response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
		return
	}
	var input struct {
		Name     string  `json:"name"`
		Grants   []Grant `json:"grants"`
		Revision int64   `json:"revision"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 200 || len(input.Grants) > 1000 {
		problem(w, 422, "invalid_installation_group")
		return
	}
	if input.Grants == nil {
		input.Grants = []Grant{}
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	// Existing installation groups use the same editor and retain any granular
	// environment grants when their administration option is turned off.
	for _, grant := range input.Grants {
		if installationAdministration(grant) {
			continue
		}
		var tenant string
		switch grant.Scope {
		case "tenant":
			err = tx.QueryRow(r.Context(), "SELECT id FROM tenants WHERE id=$1", grant.ResourceID).Scan(&tenant)
		case "site":
			err = tx.QueryRow(r.Context(), "SELECT tenant_id FROM sites WHERE id=$1", grant.ResourceID).Scan(&tenant)
		case "camera":
			err = tx.QueryRow(r.Context(), "SELECT tenant_id FROM cameras WHERE id=$1", grant.ResourceID).Scan(&tenant)
		default:
			problem(w, 422, "invalid_installation_group")
			return
		}
		if err != nil || a.validateDelegation(r.Context(), tx, current, tenant, []Grant{grant}) != nil {
			problem(w, 422, "invalid_installation_group")
			return
		}
	}
	g := Group{ID: r.PathValue("group"), Name: input.Name, Grants: input.Grants, Revision: input.Revision + 1}
	raw, _ := json.Marshal(input.Grants)
	if r.Method == "POST" {
		g.ID = newID()
		g.Revision = 1
		if _, err = tx.Exec(r.Context(), "INSERT INTO installation_groups(id,name,grants) VALUES($1,$2,$3)", g.ID, g.Name, raw); err != nil {
			dbFailure(w)
			return
		}
	} else {
		result, err := tx.Exec(r.Context(), "UPDATE installation_groups SET name=$1,grants=$2,revision=revision+1 WHERE id=$3 AND revision=$4", g.Name, raw, g.ID, input.Revision)
		if err != nil {
			dbFailure(w)
			return
		}
		if result.RowsAffected() != 1 {
			problem(w, 412, "revision_conflict")
			return
		}
	}
	if !requireInstallationAdmin(r.Context(), tx, w) {
		return
	}
	if err = audit(r.Context(), tx, current, "", "installation_group.saved", g.ID, input); err != nil {
		dbFailure(w)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		dbFailure(w)
		return
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	response(w, status, g)
}
