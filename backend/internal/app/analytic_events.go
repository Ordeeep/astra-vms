package app

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const analyticEventsAction = "events.view"
const analyticEventRetention = 90 * 24 * time.Hour

const analyticEventsMigration = `
CREATE TABLE IF NOT EXISTS analytic_events (
 id text PRIMARY KEY,
 tenant_id text NOT NULL REFERENCES tenants(id),
 camera_id text NOT NULL,
 site_id text NOT NULL,
 camera_name text NOT NULL,
 site_name text NOT NULL,
 kind text NOT NULL CHECK (kind IN ('intrusion','line_crossing')),
 rule_name text NOT NULL,
 received_at timestamptz NOT NULL,
 camera_at timestamptz,
 simulated boolean NOT NULL,
 FOREIGN KEY (tenant_id,camera_id) REFERENCES cameras(tenant_id,id),
 FOREIGN KEY (tenant_id,site_id) REFERENCES sites(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS analytic_events_history ON analytic_events(tenant_id,received_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS analytic_events_by_camera ON analytic_events(tenant_id,camera_id,received_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS analytic_events_by_site ON analytic_events(tenant_id,site_id,received_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS analytic_events_expiry ON analytic_events(received_at);
`

type analyticEvent struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	CameraID   string     `json:"cameraId"`
	CameraName string     `json:"cameraName"`
	SiteID     string     `json:"siteId"`
	SiteName   string     `json:"siteName"`
	RuleName   string     `json:"ruleName,omitempty"`
	ReceivedAt time.Time  `json:"receivedAt"`
	CameraAt   *time.Time `json:"cameraAt,omitempty"`
	Simulated  bool       `json:"simulated"`
	State      string     `json:"state,omitempty"`
	RuleID     string     `json:"ruleId,omitempty"`
}

type analyticAlert struct {
	kind, ruleName string
	cameraAt       *time.Time
	simulated      bool
}

func isAnalyticKind(kind string) bool { return kind == "intrusion" || kind == "line_crossing" }

func storeAnalyticEvent(ctx context.Context, db permissionReader, tenant, camera string, alert analyticAlert) (string, error) {
	if !isAnalyticKind(alert.kind) || len(alert.ruleName) > 512 {
		return "", errors.New("invalid analytic alert")
	}
	receivedAt := time.Now().UTC()
	var eventID string
	// Snapshot the camera and site's identity together. The row lock orders this
	// write against a concurrent move or deletion of the camera.
	err := db.QueryRow(ctx, `WITH camera AS MATERIALIZED (
 SELECT c.tenant_id,c.id,c.site_id,c.name FROM cameras c JOIN tenants t ON t.id=c.tenant_id
 WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL AND t.active FOR SHARE OF c
 ) INSERT INTO analytic_events
 (id,tenant_id,camera_id,site_id,camera_name,site_name,kind,rule_name,received_at,camera_at,simulated)
 SELECT $3,c.tenant_id,c.id,c.site_id,c.name,s.name,$4,$5,$6,$7,$8
 FROM camera c JOIN sites s ON (s.tenant_id,s.id)=(c.tenant_id,c.site_id)
 RETURNING id`, tenant, camera, newID(), alert.kind, alert.ruleName, receivedAt, alert.cameraAt, alert.simulated).Scan(&eventID)
	return eventID, err
}

type analyticScope struct {
	tenantWide bool
	sites      []string
	cameras    []string
}

func analyticScopeFor(grants []Grant, tenant string) analyticScope {
	scope := analyticScope{}
	for _, grant := range grants {
		if grant.Action != analyticEventsAction || grant.ResourceID == "" || (grant.TenantID != "" && grant.TenantID != tenant) {
			continue
		}
		switch grant.Scope {
		case "tenant":
			scope.tenantWide = scope.tenantWide || grant.ResourceID == tenant
		case "site":
			scope.sites = append(scope.sites, grant.ResourceID)
		case "camera":
			scope.cameras = append(scope.cameras, grant.ResourceID)
		}
	}
	return scope
}

func (s analyticScope) allowed(ctx context.Context, db permissionReader, tenant string) (bool, error) {
	if s.tenantWide {
		return true, nil
	}
	if len(s.sites) == 0 && len(s.cameras) == 0 {
		return false, nil
	}
	var allowed bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE tenant_id=$1 AND id=ANY($2))
 OR EXISTS(SELECT 1 FROM cameras WHERE tenant_id=$1 AND id=ANY($3))`, tenant, s.sites, s.cameras).Scan(&allowed)
	return allowed, err
}

func (a *App) analyticEventAccess(w http.ResponseWriter, r *http.Request, id identity) (string, pgx.Tx, analyticScope, bool) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return "", nil, analyticScope{}, false
	}
	tx, current, err := a.accessTransactionLock(r, id, true)
	if err != nil {
		problem(w, 403, "forbidden")
		return "", nil, analyticScope{}, false
	}
	grants, err := queryGrants(r.Context(), tx, current.User.ID, tenant)
	if err != nil {
		tx.Rollback(r.Context())
		dbFailure(w)
		return "", nil, analyticScope{}, false
	}
	scope := analyticScopeFor(grants, tenant)
	allowed, err := scope.allowed(r.Context(), tx, tenant)
	if err != nil {
		tx.Rollback(r.Context())
		dbFailure(w)
		return "", nil, analyticScope{}, false
	}
	if !allowed {
		tx.Rollback(r.Context())
		problem(w, 403, "forbidden")
		return "", nil, analyticScope{}, false
	}
	return tenant, tx, scope, true
}

type analyticQuery struct {
	from, until, before time.Time
	beforeID            string
	types, sites        []string
	cameras             []int64
	limit               int
}

func parseAnalyticQuery(values url.Values, now time.Time) (analyticQuery, bool) {
	q := analyticQuery{from: now.Add(-24 * time.Hour), until: now, limit: 30}
	from, until := values.Get("from"), values.Get("until")
	if from != "" || until != "" {
		var fromErr, untilErr error
		q.from, fromErr = time.Parse(time.RFC3339Nano, from)
		q.until, untilErr = time.Parse(time.RFC3339Nano, until)
		if fromErr != nil || untilErr != nil || !q.from.Before(q.until) {
			return q, false
		}
	}
	q.before = q.until
	if cursor := values.Get("cursor"); cursor != "" {
		at, id, valid := strings.Cut(cursor, ",")
		before, err := time.Parse(time.RFC3339Nano, at)
		decoded, idErr := hex.DecodeString(id)
		if !valid || len(decoded) != 24 || idErr != nil || err != nil || before.Before(q.from) || !before.Before(q.until) {
			return q, false
		}
		q.before, q.beforeID = before, id
	}
	if raw := values.Get("limit"); raw != "" {
		var err error
		q.limit, err = strconv.Atoi(raw)
		if err != nil || q.limit < 1 || q.limit > 100 {
			return q, false
		}
	}
	q.types, q.sites = values["type"], values["site"]
	if len(q.types) > 2 || len(q.sites) > 200 || len(values["camera"]) > 200 {
		return q, false
	}
	for _, kind := range q.types {
		if !isAnalyticKind(kind) {
			return q, false
		}
	}
	for _, site := range q.sites {
		if site == "" || len(site) > 128 {
			return q, false
		}
	}
	for _, camera := range values["camera"] {
		number := cameraNumber(camera)
		if number == 0 {
			return q, false
		}
		q.cameras = append(q.cameras, number)
	}
	return q, true
}

func (a *App) analyticEvents(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, tx, scope, ok := a.analyticEventAccess(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	query, valid := parseAnalyticQuery(r.URL.Query(), now)
	if !valid {
		problem(w, 422, "invalid_analytic_event_filter")
		return
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.kind,c.camera_number::text,e.camera_name,e.site_id,e.site_name,e.rule_name,e.received_at,e.camera_at,e.simulated,e.alert_state,e.rule_id
 FROM analytic_events e JOIN cameras c ON (c.tenant_id,c.id)=(e.tenant_id,e.camera_id)
 WHERE e.tenant_id=$1 AND e.received_at>=greatest($2::timestamptz,$3::timestamptz) AND e.received_at<$4
 AND (e.received_at,e.id)<($5,$6)
 AND ($7 OR e.site_id=ANY($8) OR e.camera_id=ANY($9))
 AND (coalesce(cardinality($10::text[]),0)=0 OR e.kind=ANY($10))
 AND (coalesce(cardinality($11::text[]),0)=0 OR e.site_id=ANY($11))
 AND (coalesce(cardinality($12::bigint[]),0)=0 OR c.camera_number=ANY($12))
 ORDER BY e.received_at DESC,e.id DESC LIMIT $13`, tenant, query.from, now.Add(-analyticEventRetention), query.until,
		query.before, query.beforeID, scope.tenantWide, scope.sites, scope.cameras, query.types, query.sites, query.cameras, query.limit+1)
	if err != nil {
		dbFailure(w)
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (analyticEvent, error) {
		var event analyticEvent
		err := row.Scan(&event.ID, &event.Type, &event.CameraID, &event.CameraName, &event.SiteID, &event.SiteName, &event.RuleName, &event.ReceivedAt, &event.CameraAt, &event.Simulated, &event.State, &event.RuleID)
		return event, err
	})
	if err != nil || tx.Commit(ctx) != nil {
		dbFailure(w)
		return
	}
	cursor := ""
	if len(items) > query.limit {
		items = items[:query.limit]
		last := items[len(items)-1]
		cursor = last.ReceivedAt.UTC().Format(time.RFC3339Nano) + "," + last.ID
	}
	response(w, 200, map[string]any{"items": items, "nextCursor": cursor, "from": query.from, "until": query.until})
}

func (a *App) analyticEventFilters(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, tx, scope, ok := a.analyticEventAccess(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	// Offer only resources covered by the current grant or represented by an
	// authorized retained event. In particular, a camera grant does not reveal
	// the sites of other cameras, and a moved camera remains a filter at its old site.
	rows, err := tx.Query(ctx, `WITH options AS MATERIALIZED (
 SELECT 'site' AS kind,s.id,CASE
  WHEN $3 OR s.id=ANY($4) OR EXISTS(SELECT 1 FROM cameras c WHERE c.tenant_id=$1 AND c.site_id=s.id AND c.deleted_at IS NULL AND c.id=ANY($5)) THEN s.name
  ELSE (SELECT e.site_name FROM analytic_events e WHERE e.tenant_id=$1 AND e.site_id=s.id AND e.received_at>=$2 AND e.camera_id=ANY($5)
    ORDER BY e.received_at DESC,e.id DESC LIMIT 1)
  END AS name
 FROM sites s WHERE s.tenant_id=$1
 UNION ALL
 SELECT 'camera',c.camera_number::text,CASE
  WHEN c.deleted_at IS NULL AND ($3 OR c.site_id=ANY($4) OR c.id=ANY($5)) THEN c.name
  ELSE (SELECT e.camera_name FROM analytic_events e WHERE e.tenant_id=$1 AND e.camera_id=c.id AND e.received_at>=$2 AND ($3 OR e.site_id=ANY($4) OR c.id=ANY($5))
    ORDER BY e.received_at DESC,e.id DESC LIMIT 1)
  END
 FROM cameras c WHERE c.tenant_id=$1
 ) SELECT kind,id,name FROM options WHERE name IS NOT NULL ORDER BY kind,name,id`, tenant, time.Now().UTC().Add(-analyticEventRetention), scope.tenantWide, scope.sites, scope.cameras)
	if err != nil {
		dbFailure(w)
		return
	}
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	options := map[string][]option{"site": {}, "camera": {}}
	for rows.Next() {
		var kind string
		var item option
		if err = rows.Scan(&kind, &item.ID, &item.Name); err != nil {
			rows.Close()
			dbFailure(w)
			return
		}
		options[kind] = append(options[kind], item)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"sites": options["site"], "cameras": options["camera"]})
}

func (a *App) pruneAnalyticEvents(ctx context.Context, now time.Time) error {
	for _, statement := range []string{
		"DELETE FROM event_collection_faults WHERE closed_at < $1",
		"DELETE FROM event_collector_outages WHERE closed_at < $1",
		"DELETE FROM event_collection_consumed WHERE applied_at < $1",
	} {
		if _, err := a.db.Exec(ctx, statement, now.Add(-analyticEventRetention)); err != nil {
			return err
		}
	}
	for {
		result, err := a.db.Exec(ctx, `DELETE FROM analytic_events WHERE id IN (
 SELECT id FROM analytic_events WHERE received_at<$1 ORDER BY received_at LIMIT 5000)`, now.Add(-analyticEventRetention))
		if err != nil {
			return err
		}
		if result.RowsAffected() < 5000 {
			return nil
		}
	}
}

func (a *App) analyticEventMaintenance() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(a.runContext, 30*time.Second)
		err := a.pruneAnalyticEvents(ctx, time.Now().UTC())
		cancel()
		if err != nil && a.runContext.Err() == nil {
			log.Print("could not prune expired analytic events")
		}
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) registerAnalyticEvents(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/analytic-events", a.protected(a.analyticEvents))
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/analytic-events/filters", a.protected(a.analyticEventFilters))
}
