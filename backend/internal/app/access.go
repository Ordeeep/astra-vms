package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Grant struct {
	Action     string `json:"action"`
	Scope      string `json:"scope"`
	ResourceID string `json:"resourceId"`
}
type Group struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Grants   []Grant `json:"grants"`
	Revision int64   `json:"revision"`
}

var actions = []string{"live", "ptz.control", "playback", "export", "inventory.manage", "camera.delete", "permissions.manage", "mosaics.manage", "recording.manage", "audit.view"}

type permissionReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (a *App) grants(ctx context.Context, user, tenant string) ([]Grant, error) {
	return queryGrants(ctx, a.db, user, tenant)
}
func queryGrants(ctx context.Context, db permissionReader, user, tenant string) ([]Grant, error) {
	rows, err := db.Query(ctx, `SELECT g.grants FROM group_members m JOIN permission_groups g ON g.id=m.group_id AND g.tenant_id=m.tenant_id JOIN operators u ON u.id=m.operator_id WHERE m.operator_id=$1 AND m.tenant_id=$2 AND u.active`, user, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Grant{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var grants []Grant
		if err := json.Unmarshal(raw, &grants); err != nil {
			return nil, err
		}
		result = append(result, grants...)
	}
	return result, rows.Err()
}
func permits(grants []Grant, tenant, action, site, camera string) bool {
	for _, g := range grants {
		if g.Action != action {
			continue
		}
		if (g.Scope == "tenant" && g.ResourceID == tenant) || (site != "" && g.Scope == "site" && g.ResourceID == site) || (camera != "" && g.Scope == "camera" && g.ResourceID == camera) {
			return true
		}
	}
	return false
}
func (a *App) allowed(ctx context.Context, id identity, tenant, action, site, camera string) bool {
	return allowedWith(ctx, a.db, id, tenant, action, site, camera)
}
func allowedWith(ctx context.Context, db permissionReader, id identity, tenant, action, site, camera string) bool {
	if id.User.GlobalAdmin && slices.Contains([]string{"inventory.manage", "camera.delete", "permissions.manage", "recording.manage", "audit.view"}, action) {
		return true
	}
	grants, err := queryGrants(ctx, db, id.User.ID, tenant)
	if err != nil {
		return false
	}
	if action == "ptz.control" && !permits(grants, tenant, "live", site, camera) {
		return false
	}
	if action == "export" && !permits(grants, tenant, "playback", site, camera) {
		return false
	}
	return permits(grants, tenant, action, site, camera)
}
func (a *App) capabilities(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	caps := map[string]bool{}
	for _, action := range actions {
		caps[action] = a.allowed(r.Context(), id, tenant, action, "", "")
	}
	response(w, 200, caps)
}
func (a *App) accessTransaction(r *http.Request, id identity) (pgx.Tx, identity, error) {
	return a.accessTransactionLock(r, id, false)
}

// Device commands read authorization concurrently; permission/inventory writers
// still take the exclusive lock, so a slow device does not starve other PTZs.
func (a *App) accessTransactionLock(r *http.Request, id identity, shared bool) (pgx.Tx, identity, error) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		return nil, id, err
	}
	lockSQL := "SELECT pg_advisory_xact_lock(78921452)"
	if shared {
		lockSQL = "SELECT pg_advisory_xact_lock_shared(78921452)"
	}
	if _, err = tx.Exec(r.Context(), lockSQL); err != nil {
		tx.Rollback(r.Context())
		return nil, id, err
	}
	err = tx.QueryRow(r.Context(), `SELECT u.id,u.username,u.active,u.global_admin,u.revision,u.name,u.email FROM operators u JOIN sessions s ON s.operator_id=u.id WHERE u.id=$1 AND s.token_hash=$2 AND s.expires>now() AND u.active`, id.User.ID, id.SessionHash).Scan(&id.User.ID, &id.User.Username, &id.User.Active, &id.User.GlobalAdmin, &id.User.Revision, &id.User.Name, &id.User.Email)
	if err != nil {
		tx.Rollback(r.Context())
		return nil, id, err
	}
	if r.PathValue("tenant") != "" && !strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		if !contextValid(r.Context(), tx, r, id) {
			tx.Rollback(r.Context())
			return nil, id, errors.New("context no longer authorized")
		}
	}
	return tx, id, nil
}
func audit(ctx context.Context, tx pgx.Tx, id identity, tenant, action, resource string, details any) error {
	raw, _ := json.Marshal(details)
	if details == nil {
		raw = []byte("{}")
	}
	var revision *int64
	prefix, _, _ := strings.Cut(action, ".")
	tables := map[string]string{"tenant": "tenants", "operator": "operators", "group": "permission_groups", "site": "sites", "camera": "cameras", "mosaic": "mosaics"}
	if table := tables[prefix]; table != "" {
		var n int64
		if err := tx.QueryRow(ctx, "SELECT revision FROM "+table+" WHERE id=$1", resource).Scan(&n); err != nil {
			return err
		}
		revision = &n
	} else if prefix == "membership" {
		var n int64
		if err := tx.QueryRow(ctx, "SELECT revision FROM memberships WHERE tenant_id=$1 AND operator_id=$2", tenant, resource).Scan(&n); err != nil {
			return err
		}
		revision = &n
	}
	_, err := tx.Exec(ctx, "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,details,revision) VALUES($1,NULLIF($2,''),$3,$4,$5,$6)", id.User.ID, tenant, action, resource, raw, revision)
	return err
}
func (a *App) adminTenants(w http.ResponseWriter, r *http.Request, id identity) {
	if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		a.me(w, r, id)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 200 {
		problem(w, 422, "invalid_name")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	tenant := Tenant{ID: newID(), Name: input.Name, Active: true, Revision: 1}
	if _, err = tx.Exec(r.Context(), "INSERT INTO tenants(id,name) VALUES($1,$2)", tenant.ID, tenant.Name); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = audit(r.Context(), tx, current, tenant.ID, "tenant.created", tenant.ID, nil); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, 201, tenant)
}
func (a *App) adminTenantUpdate(w http.ResponseWriter, r *http.Request, id identity) {
	var input struct {
		Name     string `json:"name"`
		Active   bool   `json:"active"`
		Revision int64  `json:"revision"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 200 || input.Revision < 1 {
		problem(w, 422, "invalid_tenant")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	result, err := tx.Exec(r.Context(), "UPDATE tenants SET name=$1,active=$2,revision=revision+1 WHERE id=$3 AND revision=$4", input.Name, input.Active, r.PathValue("tenant"), input.Revision)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if result.RowsAffected() != 1 {
		problem(w, 412, "revision_conflict")
		return
	}
	if !input.Active {
		if _, err = tx.Exec(r.Context(), "DELETE FROM view_contexts WHERE tenant_id=$1", r.PathValue("tenant")); err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
	}
	if err = audit(r.Context(), tx, current, r.PathValue("tenant"), "tenant.updated", r.PathValue("tenant"), map[string]any{"active": input.Active}); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if !input.Active {
		a.invalidateAvailability(r.PathValue("tenant"), "", "")
	}
	response(w, 200, Tenant{ID: r.PathValue("tenant"), Name: input.Name, Active: input.Active, Revision: input.Revision + 1})
}
func (a *App) adminOperators(w http.ResponseWriter, r *http.Request, id identity) {
	if !id.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		rows, err := a.db.Query(r.Context(), "SELECT id,username,active,global_admin,revision FROM operators WHERE id>$1 ORDER BY id LIMIT 201", r.URL.Query().Get("cursor"))
		if err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		defer rows.Close()
		users := []User{}
		for rows.Next() {
			var user User
			if rows.Scan(&user.ID, &user.Username, &user.Active, &user.GlobalAdmin, &user.Revision) != nil {
				problem(w, 503, "database_unavailable")
				return
			}
			users = append(users, user)
		}
		if rows.Err() != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		cursor := ""
		if len(users) > 200 {
			cursor = users[199].ID
			users = users[:200]
		}
		response(w, 200, map[string]any{"items": users, "nextCursor": cursor})
		return
	}
	var input struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		GlobalAdmin bool   `json:"globalAdmin"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || len(input.Username) > 128 || len(input.Password) == 0 || len(input.Password) > 1024 {
		problem(w, 422, "invalid_operator")
		return
	}
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	default:
		problem(w, 503, "authentication_busy")
		return
	}
	hash := passwordHash(input.Password)
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	user := User{ID: newID(), Username: input.Username, Name: input.Username, GlobalAdmin: input.GlobalAdmin, Active: true, Revision: 1}
	// Account identifiers share a namespace, including inactive operators.
	var identifierUsed bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM operators WHERE lower(username)=lower($1) OR lower(email)=lower($1))", input.Username).Scan(&identifierUsed); err != nil {
		dbFailure(w)
		return
	}
	if identifierUsed {
		problem(w, 409, "operator_conflict")
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO operators(id,username,password_hash,global_admin,name) VALUES($1,$2,$3,$4,$5)", user.ID, user.Username, hash, user.GlobalAdmin, user.Name); err != nil {
		problem(w, 409, "operator_conflict")
		return
	}
	if err = audit(r.Context(), tx, current, "", "operator.created", user.ID, nil); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, 201, user)
}
func (a *App) adminOperatorUpdate(w http.ResponseWriter, r *http.Request, id identity) {
	var input struct {
		Active      bool  `json:"active"`
		GlobalAdmin bool  `json:"globalAdmin"`
		Revision    int64 `json:"revision"`
	}
	if !body(w, r, &input) {
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !current.User.GlobalAdmin {
		problem(w, 403, "forbidden")
		return
	}
	result, err := tx.Exec(r.Context(), "UPDATE operators SET active=$1,global_admin=$2,revision=revision+1 WHERE id=$3 AND revision=$4", input.Active, input.GlobalAdmin, r.PathValue("operator"), input.Revision)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if result.RowsAffected() != 1 {
		problem(w, 412, "revision_conflict")
		return
	}
	if !input.Active {
		if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE operator_id=$1", r.PathValue("operator")); err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
	}
	if err = audit(r.Context(), tx, current, "", "operator.updated", r.PathValue("operator"), input); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	w.WriteHeader(204)
}
func (a *App) groups(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "permissions.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		rows, err := a.db.Query(r.Context(), "SELECT id,name,grants,revision FROM permission_groups WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT 201", tenant, r.URL.Query().Get("cursor"))
		if err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		defer rows.Close()
		groups := []Group{}
		for rows.Next() {
			var g Group
			var raw []byte
			if rows.Scan(&g.ID, &g.Name, &raw, &g.Revision) != nil || json.Unmarshal(raw, &g.Grants) != nil {
				problem(w, 503, "database_unavailable")
				return
			}
			groups = append(groups, g)
		}
		if rows.Err() != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		cursor := ""
		if len(groups) > 200 {
			cursor = groups[199].ID
			groups = groups[:200]
		}
		response(w, 200, map[string]any{"items": groups, "nextCursor": cursor})
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
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || len(input.Grants) > 1000 {
		problem(w, 422, "invalid_group")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !allowedWith(r.Context(), tx, current, tenant, "permissions.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	if err = a.validateDelegation(r.Context(), tx, current, tenant, input.Grants); err != nil {
		problem(w, 403, "grant_not_allowed")
		return
	}
	raw, _ := json.Marshal(input.Grants)
	if input.Grants == nil {
		raw = []byte("[]")
	}
	gid := r.PathValue("group")
	status := 200
	revision := input.Revision + 1
	if r.Method == "POST" {
		gid = newID()
		status = 201
		revision = 1
		_, err = tx.Exec(r.Context(), "INSERT INTO permission_groups(id,tenant_id,name,grants) VALUES($1,$2,$3,$4)", gid, tenant, input.Name, raw)
	} else {
		result, e := tx.Exec(r.Context(), "UPDATE permission_groups SET name=$1,grants=$2,revision=revision+1 WHERE id=$3 AND tenant_id=$4 AND revision=$5", input.Name, raw, gid, tenant, input.Revision)
		err = e
		if err == nil && result.RowsAffected() != 1 {
			problem(w, 412, "revision_conflict")
			return
		}
	}
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = audit(r.Context(), tx, current, tenant, "group.saved", gid, input.Grants); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, status, Group{ID: gid, Name: input.Name, Grants: input.Grants, Revision: revision})
}
func (a *App) validateDelegation(ctx context.Context, db permissionReader, id identity, tenant string, requested []Grant) error {
	owned, err := queryGrants(ctx, db, id.User.ID, tenant)
	if err != nil {
		return err
	}
	for _, g := range requested {
		if g.Action == "ptz.control" && g.Scope == "camera" {
			return errors.New("PTZ grants require tenant or site scope")
		}
		if !slices.Contains(actions, g.Action) || !slices.Contains([]string{"tenant", "site", "camera"}, g.Scope) || g.ResourceID == "" {
			return errors.New("invalid grant")
		}
		if g.Scope == "tenant" && g.ResourceID != tenant {
			return errors.New("foreign tenant")
		}
		if g.Scope != "tenant" {
			var exists bool
			table := "sites"
			if g.Scope == "camera" {
				table = "cameras"
			}
			if db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE tenant_id=$1 AND id=$2)", tenant, g.ResourceID).Scan(&exists) != nil || !exists {
				return errors.New("resource not registered")
			}
		}
		if id.User.GlobalAdmin {
			continue
		}
		covered := false
		for _, own := range owned {
			if own.Action == g.Action && ((own.Scope == "tenant" && own.ResourceID == tenant) || (own.Scope == g.Scope && own.ResourceID == g.ResourceID)) {
				covered = true
				break
			}
		}
		if !covered {
			return errors.New("grant exceeds current powers")
		}
	}
	return nil
}
func (a *App) tenantOperators(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	if !a.allowed(r.Context(), id, tenant, "permissions.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	if r.Method == "GET" {
		rows, err := a.db.Query(r.Context(), `SELECT u.id,u.username,u.active,u.global_admin,u.revision,m.revision,COALESCE((SELECT jsonb_agg(gm.group_id) FROM group_members gm WHERE gm.tenant_id=m.tenant_id AND gm.operator_id=u.id),'[]') FROM memberships m JOIN operators u ON u.id=m.operator_id WHERE m.tenant_id=$1 AND u.id>$2 ORDER BY u.id LIMIT 201`, tenant, r.URL.Query().Get("cursor"))
		if err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		defer rows.Close()
		type member struct {
			User
			MembershipRevision int64    `json:"membershipRevision"`
			Groups             []string `json:"groups"`
		}
		items := []member{}
		for rows.Next() {
			var m member
			var raw []byte
			if rows.Scan(&m.ID, &m.Username, &m.Active, &m.GlobalAdmin, &m.Revision, &m.MembershipRevision, &raw) != nil || json.Unmarshal(raw, &m.Groups) != nil {
				problem(w, 503, "database_unavailable")
				return
			}
			items = append(items, m)
		}
		if rows.Err() != nil {
			problem(w, 503, "database_unavailable")
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
		Groups   []string `json:"groups"`
		Revision int64    `json:"revision"`
	}
	if !body(w, r, &input) {
		return
	}
	if len(input.Groups) > 100 {
		problem(w, 422, "too_many_groups")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if !allowedWith(r.Context(), tx, current, tenant, "permissions.manage", "", "") {
		problem(w, 403, "forbidden")
		return
	}
	operator := r.PathValue("operator")
	var revision int64
	err = tx.QueryRow(r.Context(), "SELECT revision FROM memberships WHERE tenant_id=$1 AND operator_id=$2", tenant, operator).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "database_unavailable")
		return
	}
	if revision != input.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	if !current.User.GlobalAdmin {
		var member bool
		if tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND operator_id=$2)", tenant, operator).Scan(&member) != nil || !member {
			problem(w, 404, "operator_not_found")
			return
		}
	}
	for _, group := range input.Groups {
		var raw []byte
		if err = tx.QueryRow(r.Context(), "SELECT grants FROM permission_groups WHERE id=$1 AND tenant_id=$2", group, tenant).Scan(&raw); err != nil {
			problem(w, 404, "group_not_found")
			return
		}
		var grants []Grant
		if json.Unmarshal(raw, &grants) != nil || a.validateDelegation(r.Context(), tx, current, tenant, grants) != nil {
			problem(w, 403, "grant_not_allowed")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO memberships(tenant_id,operator_id) VALUES($1,$2) ON CONFLICT(tenant_id,operator_id) DO UPDATE SET revision=memberships.revision+1", tenant, operator); err != nil {
		problem(w, 404, "operator_not_found")
		return
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM group_members WHERE tenant_id=$1 AND operator_id=$2", tenant, operator); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	for _, group := range input.Groups {
		if _, err = tx.Exec(r.Context(), "INSERT INTO group_members(tenant_id,operator_id,group_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", tenant, operator, group); err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
	}
	if err = audit(r.Context(), tx, current, tenant, "membership.groups_changed", operator, input); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, 200, map[string]any{"operatorId": operator, "groups": input.Groups, "revision": revision + 1})
}
func (a *App) registerAccess(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/tenants", a.protected(a.adminTenants))
	mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenant}", a.protected(a.adminTenantUpdate))
	mux.HandleFunc("GET /api/v1/admin/operators", a.protected(a.adminOperators))
	mux.HandleFunc("POST /api/v1/admin/operators", a.protected(a.adminOperators))
	mux.HandleFunc("PATCH /api/v1/admin/operators/{operator}", a.protected(a.adminOperatorUpdate))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/capabilities", a.protected(a.capabilities))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/groups", a.protected(a.groups))
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/groups", a.protected(a.groups))
	mux.HandleFunc("PUT /api/v1/tenants/{tenant}/groups/{group}", a.protected(a.groups))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/operators", a.protected(a.tenantOperators))
	mux.HandleFunc("PUT /api/v1/tenants/{tenant}/operators/{operator}", a.protected(a.tenantOperators))
}
