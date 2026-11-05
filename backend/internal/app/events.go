package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"vms.local/backend/internal/media"
)

//go:embed events.sql
var eventsSQL string

type notification struct {
	EventID       string          `json:"eventId"`
	ContextID     string          `json:"contextId"`
	TenantID      string          `json:"tenantId"`
	ResourceID    string          `json:"resourceId"`
	Revision      int64           `json:"resourceRevision"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlationId"`
	Payload       json.RawMessage `json:"payload"`
	camera, site  string
	sequence      int64
}

func (a *App) dispatchEvents() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var passes int
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(a.runContext, time.Second)
			// One core owns this database. Rows committed later remain pending and
			// receive later delivery IDs, even when their identity ID is smaller.
			_, err := a.db.Exec(ctx, `WITH pending AS MATERIALIZED (SELECT id FROM event_outbox WHERE delivery IS NULL ORDER BY id LIMIT 500)
 UPDATE event_outbox e SET delivery=nextval('event_delivery_sequence') FROM pending p WHERE e.id=p.id`)
			passes++
			if err == nil && passes%100 == 0 {
				a.db.Exec(ctx, `DELETE FROM event_outbox WHERE id IN (SELECT id FROM event_outbox WHERE delivery IS NOT NULL AND
 (occurred_at<now()-interval '1 hour' OR delivery<(SELECT coalesce(max(delivery),0)-100000 FROM event_outbox)) LIMIT 2000)`)
			}
			cancel()
		}
	}
}

func (a *App) eventStream(w http.ResponseWriter, r *http.Request, id identity) {
	// EventSource cannot set a custom header. Context is not a credential and
	// is still bound to the HttpOnly session cookie in every authorization pass.
	r.Header.Set("X-VMS-Context", r.URL.Query().Get("context"))
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	select {
	case a.eventSlots <- struct{}{}:
		defer func() { <-a.eventSlots }()
	default:
		problem(w, 429, "event_capacity")
		return
	}
	contextID := r.Header.Get("X-VMS-Context")
	last := int64(0)
	cursor := r.Header.Get("Last-Event-ID")
	if cursor != "" {
		prefix, number, valid := strings.Cut(cursor, "/")
		n, err := strconv.ParseInt(number, 10, 64)
		if !valid || prefix != contextID || err != nil || n < 0 {
			problem(w, 422, "invalid_event_cursor")
			return
		}
		last = n
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(a.runContext, cancel)
	defer stop()
	control := http.NewResponseController(w)
	interrupted := make(chan struct{})
	interrupt := context.AfterFunc(ctx, func() { control.SetWriteDeadline(time.Now()); close(interrupted) })
	defer func() {
		if !interrupt() {
			<-interrupted
		}
		control.SetWriteDeadline(time.Time{})
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(event notification) bool {
		event.ContextID = contextID
		event.TenantID = tenant
		if event.EventID == "" {
			event.EventID = contextID + "/" + strconv.FormatInt(last, 10)
		}
		if event.CorrelationID == "" {
			event.CorrelationID = newID()
		}
		if event.OccurredAt.IsZero() {
			event.OccurredAt = time.Now().UTC()
		}
		if event.Payload == nil {
			event.Payload = json.RawMessage(`{}`)
		}
		raw, err := json.Marshal(event)
		if err != nil || control.SetWriteDeadline(time.Now().Add(750*time.Millisecond)) != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.EventID, raw)
		if err != nil || control.Flush() != nil {
			return false
		}
		// HTTP/2 expires an outstanding write deadline even while idle.
		// Bound each delivery, then clear it while waiting for the next event.
		return ctx.Err() == nil && control.SetWriteDeadline(time.Time{}) == nil
	}
	check, cancelCheck := context.WithTimeout(ctx, 400*time.Millisecond)
	var oldest, newest int64
	err := a.db.QueryRow(check, `SELECT coalesce(min(delivery),0),coalesce(max(delivery),0) FROM event_outbox WHERE delivery IS NOT NULL`).Scan(&oldest, &newest)
	cancelCheck()
	if err != nil {
		return
	}
	resync := cursor == "" || last < oldest-1 || last > newest
	if resync {
		last = newest
	}
	if !write(notification{Type: map[bool]string{true: "ResyncRequired", false: "Connected"}[resync], ResourceID: tenant}) {
		return
	}
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	fingerprint := ""
	for {
		check, done := context.WithTimeout(ctx, 400*time.Millisecond)
		// Refresh global-admin state as well as memberships; a snapshot identity
		// must not keep administrative access after the account is changed.
		current := id
		err := a.db.QueryRow(check, `SELECT o.global_admin FROM operators o JOIN sessions s ON s.operator_id=o.id
 WHERE o.id=$1 AND o.active AND s.token_hash=$2 AND s.expires>now()`, id.User.ID, id.SessionHash).Scan(&current.User.GlobalAdmin)
		if err != nil || !contextValid(check, a.db, r, current) {
			done()
			write(notification{Type: "ContextClosed", ResourceID: contextID})
			return
		}
		grants, err := a.grants(check, current.User.ID, tenant)
		if err != nil {
			done()
			return
		}
		slices.SortFunc(grants, func(x, y Grant) int {
			return strings.Compare(x.Action+"/"+x.Scope+"/"+x.ResourceID, y.Action+"/"+y.Scope+"/"+y.ResourceID)
		})
		raw, _ := json.Marshal(struct {
			Admin  bool
			Grants []Grant
		}{current.User.GlobalAdmin, grants})
		nextFingerprint := digest(string(raw))
		changed := fingerprint != "" && nextFingerprint != fingerprint
		fingerprint = nextFingerprint
		if a.db.QueryRow(check, `SELECT coalesce(min(delivery),0),coalesce(max(delivery),0) FROM event_outbox WHERE delivery IS NOT NULL`).Scan(&oldest, &newest) != nil {
			done()
			return
		}
		if last < oldest-1 {
			last = newest
			if !write(notification{Type: "ResyncRequired", ResourceID: tenant}) {
				done()
				return
			}
		}
		rows, err := a.db.Query(check, `SELECT delivery,resource_id,camera_id,site_id,revision,kind,payload,occurred_at,correlation_id
 FROM event_outbox WHERE tenant_id=$1 AND delivery>$2 ORDER BY delivery LIMIT 100`, tenant, last)
		if err != nil {
			done()
			return
		}
		batch := make([]notification, 0, 100)
		for rows.Next() {
			var e notification
			if err = rows.Scan(&e.sequence, &e.ResourceID, &e.camera, &e.site, &e.Revision, &e.Type, &e.Payload, &e.OccurredAt, &e.CorrelationID); err != nil {
				break
			}
			batch = append(batch, e)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			done()
			return
		}
		if changed && !write(notification{Type: "AuthorizationChanged", ResourceID: tenant}) {
			done()
			return
		}
		for _, e := range batch {
			last = e.sequence
			if !a.eventAuthorized(check, current, tenant, grants, e) {
				continue
			}
			// No client queue: a bounded batch and bounded socket write. Check
			// the deadline before each send; slow clients must reconnect.
			if check.Err() != nil {
				done()
				return
			}
			if e.Revision == 0 {
				e.Revision = e.sequence
			}
			if !write(e) {
				done()
				return
			}
		}
		done()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) eventAuthorized(ctx context.Context, id identity, tenant string, grants []Grant, e notification) bool {
	has := func(action string) bool { return permits(grants, tenant, action, e.site, e.camera) }
	switch e.Type {
	case "ArchiveChanged":
		return has("playback")
	case "CameraStatusChanged":
		// This event describes the current camera, unlike archive events whose
		// site is historical provenance. A replay must use its current location.
		if a.db.QueryRow(ctx, "SELECT site_id FROM cameras WHERE tenant_id=$1 AND id=$2", tenant, e.camera).Scan(&e.site) != nil {
			return false
		}
		return id.User.GlobalAdmin || has("inventory.manage") || has("camera.delete") || has("playback") || has("live") || has("recording.manage")
	case "CaptureStateChanged":
		return id.User.GlobalAdmin || has("recording.manage") || has("audit.view") || has("playback")
	case "StoragePressure":
		return id.User.GlobalAdmin || has("recording.manage") || has("audit.view")
	case "ExportStateChanged":
		var raw []byte
		if a.db.QueryRow(ctx, "SELECT plan FROM export_jobs WHERE id=$1 AND tenant_id=$2", e.ResourceID, tenant).Scan(&raw) != nil {
			return false
		}
		var plan media.ExportPlan
		if json.Unmarshal(raw, &plan) != nil || len(plan.Clips) == 0 {
			return false
		}
		for _, clip := range plan.Clips {
			if !permits(grants, tenant, "playback", clip.Index.Site, clip.Index.Camera) || !permits(grants, tenant, "export", clip.Index.Site, clip.Index.Camera) {
				return false
			}
		}
		return true
	}
	return false
}
