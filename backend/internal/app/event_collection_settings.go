package app

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/eventcollector"
)

type collectionSetting struct {
	Enabled  bool                  `json:"enabled"`
	Revision int64                 `json:"revision"`
	Rules    []eventcollector.Rule `json:"rules"`
}
type settingsAccess struct {
	tx                     pgx.Tx
	actor                  identity
	tenant, camera, source string
}

func (a *App) collectionAccess(w http.ResponseWriter, r *http.Request, id identity) (settingsAccess, bool) {
	var access settingsAccess
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return access, false
	}
	cid, ok := a.requestCameraKey(w, r, tenant, r.PathValue("camera"))
	if !ok {
		return access, false
	}
	ctx := r.Context()
	tx, current, err := a.accessTransactionLock(r, id, true)
	if err != nil {
		problem(w, 403, "forbidden")
		return access, false
	}
	var site, source, kind string
	query := `SELECT c.site_id,c.source_version,v.connection_type FROM cameras c JOIN camera_sources v ON v.id=c.source_version WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL`
	if r.Method == "PUT" {
		query += " FOR UPDATE OF c"
	}
	err = tx.QueryRow(ctx, query, tenant, cid).Scan(&site, &source, &kind)
	if err != nil || !allowedWith(ctx, tx, current, tenant, "inventory.manage", site, cid) {
		tx.Rollback(ctx)
		problem(w, 403, "forbidden")
		return access, false
	}
	if _, ok = camera.Events(kind); !ok {
		tx.Rollback(ctx)
		problem(w, 422, "unsupported_event_integration")
		return access, false
	}
	return settingsAccess{tx, current, tenant, cid, source}, true
}
func validRules(rules []eventcollector.Rule) bool {
	if len(rules) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range rules {
		key := r.Kind + "/" + r.ID
		if seen[key] || !isAnalyticKind(r.Kind) || len(r.ID) > 128 || strings.ContainsAny(r.ID, "/\x00\r\n") || len(r.Label) > 512 || len(r.Schedule) > 512 || strings.ContainsRune(r.Label+r.Schedule, 0) {
			return false
		}
		seen[key] = true
	}
	return true
}
func (a *App) eventSettings(w http.ResponseWriter, r *http.Request, id identity) {
	x, ok := a.collectionAccess(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	defer x.tx.Rollback(ctx)
	current := collectionSetting{Rules: []eventcollector.Rule{}}
	var previousSource string
	err := x.tx.QueryRow(ctx, `SELECT enabled,revision,rules,source_id FROM event_collection_current WHERE tenant_id=$1 AND camera_id=$2`, x.tenant, x.camera).Scan(&current.Enabled, &current.Revision, &current.Rules, &previousSource)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		dbFailure(w)
		return
	}
	if previousSource != "" && previousSource != x.source {
		current.Enabled = false
		current.Rules = []eventcollector.Rule{}
	}
	if r.Method == "GET" {
		response(w, 200, current)
		return
	}
	var in collectionSetting
	if !body(w, r, &in) {
		return
	}
	if in.Revision != current.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	if !validRules(in.Rules) {
		problem(w, 422, "invalid_supervised_rules")
		return
	}
	if in.Rules == nil {
		in.Rules = []eventcollector.Rule{}
	}
	if previousSource == x.source && current.Enabled == in.Enabled && slices.Equal(current.Rules, in.Rules) {
		response(w, 200, current)
		return
	}
	in.Revision++
	var at time.Time
	err = x.tx.QueryRow(ctx, `INSERT INTO event_collection_settings(tenant_id,camera_id,revision,source_id,enabled,rules) VALUES($1,$2,$3,$4,$5,$6) RETURNING changed_at`, x.tenant, x.camera, in.Revision, x.source, in.Enabled, in.Rules).Scan(&at)
	if err == nil {
		_, err = x.tx.Exec(ctx, `UPDATE event_collection_faults SET closed_at=greatest(opened_at,$3),end_reason=CASE
 WHEN NOT $4 OR (condition LIKE 'rule/%' AND NOT EXISTS (SELECT FROM jsonb_array_elements($5::jsonb) r WHERE condition='rule/'||(r->>'kind')||'/'||(r->>'id'))) THEN 'suspended'
 ELSE 'configuration_changed' END WHERE tenant_id=$1 AND camera_id=$2 AND closed_at IS NULL`, x.tenant, x.camera, at, in.Enabled, in.Rules)
	}
	if err == nil {
		err = audit(ctx, x.tx, x.actor, x.tenant, "camera.event_supervision_changed", x.camera, map[string]any{"enabled": in.Enabled, "revision": in.Revision, "rules": len(in.Rules)})
	}
	if err != nil || x.tx.Commit(ctx) != nil {
		dbFailure(w)
		return
	}
	response(w, 200, in)
}
func (a *App) readEventRules(w http.ResponseWriter, r *http.Request, id identity) {
	x, ok := a.collectionAccess(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	defer x.tx.Rollback(ctx)
	s, err := a.loadCameraSourceFrom(ctx, x.tx, x.tenant, x.camera, x.source)
	if err != nil {
		dbFailure(w)
		return
	}
	receiver, ok := camera.Events(s.Type)
	if !ok {
		problem(w, 422, "unsupported_event_integration")
		return
	}
	// A bounded probe slot limits device requests from concurrent operators.
	select {
	case a.probeSlots <- struct{}{}:
		defer func() { <-a.probeSlots }()
	default:
		problem(w, 429, "probe_busy")
		return
	}
	inventories := map[string]camera.RuleInventory{}
	for _, kind := range []string{"intrusion", "line_crossing"} {
		v, e := receiver.RuleInventory(ctx, s, kind)
		if e == nil {
			inventories[kind] = v
		}
	}
	device, _ := receiver.EventDevice(ctx, s)
	qualification, err := lookupEventQualification(a.opts.DataDir, device)
	if err != nil {
		problem(w, 503, "qualification_unavailable")
		return
	}
	response(w, 200, map[string]any{"inventory": inventories, "device": device, "qualification": qualification})
}

type collectionFault struct {
	ID        string     `json:"id"`
	Camera    string     `json:"camera"`
	Condition string     `json:"condition"`
	Code      string     `json:"code"`
	Opened    time.Time  `json:"openedAt"`
	Closed    *time.Time `json:"closedAt"`
	Reason    string     `json:"endReason"`
	Affected  []string   `json:"affected,omitempty"`
}

func (a *App) eventFailures(w http.ResponseWriter, r *http.Request, id identity) {
	t, tx, s, ok := a.analyticEventAccess(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	history := r.URL.Query().Get("closed") == "1"
	var before *time.Time
	var beforeID string
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		raw, key, found := strings.Cut(cursor, "|")
		at, e := time.Parse(time.RFC3339Nano, raw)
		if !found || e != nil || key == "" || len(key) > 128 {
			problem(w, 422, "invalid_cursor")
			return
		}
		before = &at
		beforeID = key
	}
	rows, err := tx.Query(ctx, `SELECT id,camera,condition,code,opened_at,closed_at,end_reason,affected FROM (
 SELECT f.id,c.camera_number::text AS camera,f.condition,f.code,f.opened_at,f.closed_at,f.end_reason,'{}'::text[] AS affected
 FROM event_collection_faults f JOIN cameras c ON (c.tenant_id,c.id)=(f.tenant_id,f.camera_id)
 WHERE f.tenant_id=$1 AND ($2 OR f.site_id=ANY($3) OR f.camera_id=ANY($4))
 UNION ALL
 SELECT 'collector/'||o.id,'','collector','collector_unreachable',o.opened_at,o.closed_at,CASE WHEN o.closed_at IS NULL THEN '' ELSE 'restored' END,array_agg(DISTINCT c.camera_number::text ORDER BY c.camera_number::text)
 FROM event_collector_outages o JOIN event_collector_affected af ON o.id=af.outage JOIN cameras c ON (c.tenant_id,c.id)=(af.tenant_id,af.camera_id)
 WHERE af.tenant_id=$1 AND ($2 OR af.site_id=ANY($3) OR af.camera_id=ANY($4)) GROUP BY o.id
 ) f WHERE (($5 AND closed_at >= now()-interval '90 days') OR (NOT $5 AND closed_at IS NULL))
 AND ($6::timestamptz IS NULL OR (COALESCE(closed_at,opened_at),id)<($6,$7))
 ORDER BY COALESCE(closed_at,opened_at) DESC,id DESC LIMIT 101`, t, s.tenantWide, s.sites, s.cameras, history, before, beforeID)
	if err != nil {
		dbFailure(w)
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (collectionFault, error) {
		var f collectionFault
		e := row.Scan(&f.ID, &f.Camera, &f.Condition, &f.Code, &f.Opened, &f.Closed, &f.Reason, &f.Affected)
		return f, e
	})
	if err != nil || tx.Commit(ctx) != nil {
		dbFailure(w)
		return
	}
	more := len(items) > 100
	cursor := ""
	if more {
		items = items[:100]
		last := items[len(items)-1]
		at := last.Opened
		if last.Closed != nil {
			at = *last.Closed
		}
		cursor = at.UTC().Format(time.RFC3339Nano) + "|" + last.ID
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}
