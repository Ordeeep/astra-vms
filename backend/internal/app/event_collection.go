package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/eventcollector"
)

const eventCollectionMigration = `
ALTER TABLE analytic_events ADD COLUMN alert_state text NOT NULL DEFAULT '', ADD COLUMN rule_id text NOT NULL DEFAULT '';
CREATE TABLE event_collection_settings(
 tenant_id text NOT NULL,camera_id text NOT NULL,revision bigint NOT NULL,source_id text NOT NULL,
 enabled boolean NOT NULL,rules jsonb NOT NULL,changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,camera_id,revision),FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE VIEW event_collection_current AS SELECT DISTINCT ON(tenant_id,camera_id) * FROM event_collection_settings ORDER BY tenant_id,camera_id,revision DESC;
CREATE TABLE event_collection_faults(
 id text PRIMARY KEY,tenant_id text NOT NULL,camera_id text NOT NULL,site_id text NOT NULL,revision bigint NOT NULL,
 condition text NOT NULL,code text NOT NULL,opened_at timestamptz NOT NULL,closed_at timestamptz,end_reason text NOT NULL DEFAULT '',
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE UNIQUE INDEX event_fault_open ON event_collection_faults(tenant_id,camera_id,revision,condition) WHERE closed_at IS NULL;
CREATE FUNCTION close_deleted_event_faults() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE event_collection_faults SET closed_at=greatest(opened_at,NEW.deleted_at),end_reason='deleted'
 WHERE tenant_id=NEW.tenant_id AND camera_id=NEW.id AND closed_at IS NULL;
 RETURN NEW;
END $$;
CREATE TRIGGER close_deleted_event_faults AFTER UPDATE OF deleted_at ON cameras FOR EACH ROW
 WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) EXECUTE FUNCTION close_deleted_event_faults();
CREATE INDEX event_fault_scope ON event_collection_faults(tenant_id,closed_at,opened_at DESC);
CREATE INDEX event_location_at ON camera_locations(tenant_id,camera_id,effective_at DESC,id DESC);
CREATE TABLE event_camera_names (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, tenant_id text NOT NULL,camera_id text NOT NULL,
 name text NOT NULL,effective_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO event_camera_names(tenant_id,camera_id,name,effective_at) SELECT tenant_id,id,name,'-infinity' FROM cameras;
CREATE INDEX event_camera_name_at ON event_camera_names(tenant_id,camera_id,effective_at DESC,id DESC);
CREATE FUNCTION event_remember_camera_name() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.name IS DISTINCT FROM OLD.name THEN
  INSERT INTO event_camera_names(tenant_id,camera_id,name) VALUES(NEW.tenant_id,NEW.id,NEW.name);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER event_camera_name AFTER INSERT OR UPDATE OF name ON cameras FOR EACH ROW EXECUTE FUNCTION event_remember_camera_name();
CREATE TABLE event_collector_health(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),last_contact timestamptz,pressure boolean NOT NULL DEFAULT false,bytes bigint NOT NULL DEFAULT 0);
INSERT INTO event_collector_health(singleton) VALUES(true);
CREATE TABLE event_collector_outages(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,opened_at timestamptz NOT NULL,closed_at timestamptz);
CREATE UNIQUE INDEX event_collector_single_outage ON event_collector_outages((true)) WHERE closed_at IS NULL;
CREATE TABLE event_collector_affected(outage bigint NOT NULL REFERENCES event_collector_outages(id) ON DELETE CASCADE,tenant_id text NOT NULL,site_id text NOT NULL,camera_id text NOT NULL,PRIMARY KEY(outage,tenant_id,site_id,camera_id));
CREATE TABLE event_collection_consumed(id text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now());
`

func (a *App) assignments(ctx context.Context) ([]eventcollector.Assignment, error) {
	rows, e := a.db.Query(ctx, `SELECT ec.tenant_id,ec.camera_id,ec.source_id,ec.revision,ec.rules,v.secrets
 FROM event_collection_current ec JOIN cameras c ON (ec.tenant_id,ec.camera_id)=(c.tenant_id,c.id) AND c.source_version=ec.source_id
 JOIN camera_sources v ON v.id=ec.source_id JOIN sites s ON s.id=c.site_id JOIN tenants t ON t.id=c.tenant_id
 WHERE ec.enabled AND c.deleted_at IS NULL AND c.active AND s.active AND t.active AND v.connection_type=ANY($1) ORDER BY ec.tenant_id,ec.camera_id`, camera.EventIntegrations())
	if e != nil {
		return nil, e
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (eventcollector.Assignment, error) {
		var config eventcollector.Assignment
		var ciphertext []byte
		if err := row.Scan(&config.Tenant, &config.Camera, &config.SourceID, &config.Revision, &config.Rules, &ciphertext); err != nil {
			return config, err
		}
		plain, err := a.unseal(ciphertext, config.Tenant+"/"+config.Camera+"/"+config.SourceID)
		if err != nil {
			return config, err
		}
		err = json.Unmarshal(plain, &config.Source)
		return config, err
	})
}
func (a *App) collectorCall(ctx context.Context, method, path string, in, out any) error {
	var data []byte
	var err error
	if in != nil {
		data, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, a.opts.EventWorkerURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.opts.WorkerSecret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return errors.New("event worker unavailable")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
	}
	return nil
}
func (a *App) noteCollectorContact(ctx context.Context, batch *eventcollector.Batch) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Preserve an observation gap even when the Core itself was stopped until recovery.
	_, err = tx.Exec(ctx, `INSERT INTO event_collector_outages(opened_at)
 SELECT COALESCE(last_contact,now()) FROM event_collector_health
 WHERE (last_contact IS NULL AND $1) OR last_contact<now()-interval '15 seconds' ON CONFLICT DO NOTHING`, batch == nil)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO event_collector_affected
 SELECT o.id,c.tenant_id,c.site_id,c.id FROM event_collector_outages o
 CROSS JOIN event_collection_current ec JOIN cameras c ON (c.tenant_id,c.id)=(ec.tenant_id,ec.camera_id)
 JOIN sites s ON s.id=c.site_id JOIN tenants t ON t.id=c.tenant_id
 WHERE o.closed_at IS NULL AND ec.enabled AND c.source_version=ec.source_id AND c.deleted_at IS NULL AND c.active AND t.active AND s.active
 ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if batch != nil {
		if _, err = tx.Exec(ctx, `UPDATE event_collector_health SET last_contact=clock_timestamp(),pressure=$1,bytes=$2`, batch.Pressure, batch.Bytes); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE event_collector_outages SET closed_at=clock_timestamp() WHERE closed_at IS NULL`); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *App) syncEventCollector(ctx context.Context) error {
	var batch eventcollector.Batch
	if err := a.collectorCall(ctx, "GET", "/batch", nil, &batch); err != nil {
		return a.noteCollectorContact(ctx, nil)
	}
	if err := a.noteCollectorContact(ctx, &batch); err != nil {
		return err
	}
	cfg, configErr := a.assignments(ctx)
	if configErr == nil {
		configErr = a.collectorCall(ctx, "PUT", "/config", cfg, nil)
	}
	// A rejected configuration must not prevent delivery from the last valid session.
	for len(batch.Items) > 0 {
		ids, err := a.consumeEventBatch(ctx, batch.Items)
		if err != nil {
			return err
		}
		if err = a.collectorCall(ctx, "POST", "/ack", ids, nil); err != nil {
			return err
		}
		if len(ids) < 128 {
			break
		}
		if err = a.collectorCall(ctx, "GET", "/batch", nil, &batch); err != nil {
			return err
		}
		if err = a.noteCollectorContact(ctx, &batch); err != nil {
			return err
		}
	}
	return configErr
}
func (a *App) eventCollectorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(a.runContext, 25*time.Second)
		if err := a.syncEventCollector(ctx); err != nil && a.runContext.Err() == nil {
			log.Print("event collector synchronization pending")
		}
		cancel()
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) consumeEventBatch(ctx context.Context, items []eventcollector.Record) ([]string, error) {
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	ids := make([]string, 0, len(items))
	for _, r := range items {
		if e = consumeEvent(ctx, tx, r); e != nil {
			return nil, e
		}
		ids = append(ids, r.ID)
	}
	return ids, tx.Commit(ctx)
}
func consumeEvent(ctx context.Context, tx pgx.Tx, r eventcollector.Record) error {
	if len(r.ID) != 48 || r.At.IsZero() {
		return errors.New("invalid collector record")
	}
	// Ownership and cutoffs are resolved by the Core, including for a camera that has since moved or been deleted.
	var site, cameraName, siteName string
	var deleted, nextConfig *time.Time
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT l.site_id,n.name,s.name,c.deleted_at,ec.enabled,
 (SELECT min(changed_at) FROM event_collection_settings n WHERE n.tenant_id=ec.tenant_id AND n.camera_id=ec.camera_id AND n.revision>ec.revision)
 FROM cameras c JOIN event_collection_settings ec ON ec.camera_id=c.id AND ec.tenant_id=c.tenant_id AND ec.revision=$3
 JOIN LATERAL (SELECT site_id FROM camera_locations WHERE tenant_id=c.tenant_id AND camera_id=c.id AND effective_at<=$4 ORDER BY effective_at DESC,id DESC LIMIT 1) l ON true
 JOIN sites s ON s.id=l.site_id
 JOIN LATERAL (SELECT name FROM event_camera_names WHERE tenant_id=c.tenant_id AND camera_id=c.id AND effective_at<=$4 ORDER BY effective_at DESC,id DESC LIMIT 1) n ON true
 WHERE c.tenant_id=$1 AND c.id=$2 FOR SHARE OF c`, r.Tenant, r.Camera, r.Revision, r.At).Scan(&site, &cameraName, &siteName, &deleted, &enabled, &nextConfig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if deleted != nil && !r.At.Before(*deleted) {
		_, err = tx.Exec(ctx, `INSERT INTO event_collection_faults(id,tenant_id,camera_id,site_id,revision,condition,code,opened_at,closed_at,end_reason) VALUES($1,$2,$3,$4,$5,'deleted','collection_after_deletion',$6,$7,'deleted') ON CONFLICT(id) DO UPDATE SET closed_at=greatest(event_collection_faults.closed_at,EXCLUDED.closed_at)`, "deleted/"+r.Camera, r.Tenant, r.Camera, site, r.Revision, *deleted, r.At)
		return err
	}
	if r.Alert != nil {
		v := r.Alert
		if !isAnalyticKind(v.Kind) || r.At.Before(time.Now().Add(-analyticEventRetention)) {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO analytic_events(id,tenant_id,camera_id,site_id,camera_name,site_name,kind,rule_name,received_at,camera_at,simulated,alert_state,rule_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,false,$11,$12) ON CONFLICT(id) DO NOTHING`, r.ID, r.Tenant, r.Camera, site, cameraName, siteName, v.Kind, v.RuleName, r.At, v.CameraAt, v.State, v.RuleID)
		return err
	}
	if r.Condition == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, "INSERT INTO event_collection_consumed(id) VALUES($1) ON CONFLICT DO NOTHING", r.ID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	// A queued recovery can cross a settings change. It belongs only to that revision.
	if nextConfig != nil && !r.At.Before(*nextConfig) {
		return nil
	}
	if r.Condition == "lifecycle" {
		_, err = tx.Exec(ctx, `UPDATE event_collection_faults SET closed_at=greatest(opened_at,$4),end_reason='suspended' WHERE tenant_id=$1 AND camera_id=$2 AND revision=$3 AND closed_at IS NULL`, r.Tenant, r.Camera, r.Revision, r.At)
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE event_collection_faults SET closed_at=greatest(opened_at,$5),end_reason=$6 WHERE tenant_id=$1 AND camera_id=$2 AND revision=$3 AND condition=$4 AND (closed_at IS NULL OR (closed_at>$5 AND end_reason IN ('configuration_changed','suspended','deleted')))`, r.Tenant, r.Camera, r.Revision, r.Condition, r.At, faultEndReason(r.Code))
	if err != nil || r.Code == "" {
		return err
	}
	cutoff := nextConfig
	reason := ""
	if cutoff != nil {
		reason = "configuration_changed"
		var stillSupervised bool
		if err = tx.QueryRow(ctx, `SELECT enabled AND ($4::text NOT LIKE 'rule/%' OR EXISTS (
 SELECT FROM jsonb_array_elements(rules) item WHERE $4='rule/'||(item->>'kind')||'/'||(item->>'id')))
 FROM event_collection_settings WHERE tenant_id=$1 AND camera_id=$2 AND revision>$3 ORDER BY revision LIMIT 1`, r.Tenant, r.Camera, r.Revision, r.Condition).Scan(&stillSupervised); err != nil {
			return err
		}
		if !stillSupervised {
			reason = "suspended"
		}
	}
	if deleted != nil && (cutoff == nil || deleted.Before(*cutoff)) {
		cutoff = deleted
		reason = "deleted"
	}
	_, err = tx.Exec(ctx, `INSERT INTO event_collection_faults(id,tenant_id,camera_id,site_id,revision,condition,code,opened_at,closed_at,end_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.Tenant, r.Camera, site, r.Revision, r.Condition, r.Code, r.At, cutoff, reason)
	return err
}
func faultEndReason(code string) string {
	if code == "" {
		return "restored"
	}
	return "changed"
}

func (a *App) collectorHealth(w http.ResponseWriter, r *http.Request) {
	var at *time.Time
	var pressure bool
	if err := a.db.QueryRow(r.Context(), "SELECT last_contact,pressure FROM event_collector_health").Scan(&at, &pressure); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	fresh := at != nil && time.Since(*at) < 20*time.Second
	status := 200
	if !fresh || pressure {
		status = 503
	}
	response(w, status, map[string]any{"connected": fresh, "queuePressure": pressure, "lastContact": at})
}
func (a *App) registerEventCollection(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/health/event-collector", a.collectorHealth)
	base := "/api/v1/tenants/{tenant}/cameras/{camera}/event-collection"
	for _, method := range []string{"GET", "PUT"} {
		m.HandleFunc(method+" "+base, a.protected(a.eventSettings))
	}
	m.HandleFunc("GET "+base+"/rules", a.protected(a.readEventRules))
	m.HandleFunc("GET /api/v1/tenants/{tenant}/analytic-events/failures", a.protected(a.eventFailures))
}
