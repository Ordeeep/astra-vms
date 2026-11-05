package app

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

type Position struct {
	CameraID string   `json:"cameraId"`
	Profile  string   `json:"profile"`
	Fallback []string `json:"fallback,omitempty"`
}
type Mosaic struct {
	ID         string     `json:"id"`
	Owner      string     `json:"ownerId"`
	Name       string     `json:"name"`
	Visibility string     `json:"visibility"`
	Positions  []Position `json:"positions"`
	Revision   int64      `json:"revision"`
	Editable   bool       `json:"editable"`
}
type QualityRule struct {
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	Profile  string   `json:"profile"`
	Fallback []string `json:"fallback"`
}

var defaultQuality = []QualityRule{{Min: 1, Max: 2, Profile: "main", Fallback: []string{}}, {Min: 3, Max: 4, Profile: "sub2", Fallback: []string{"main"}}, {Min: 5, Max: 16, Profile: "sub3", Fallback: []string{"sub2", "main"}}}

func (a *App) mosaics(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	mid := r.PathValue("mosaic")
	if r.Method == "GET" {
		manage := a.allowed(r.Context(), id, tenant, "mosaics.manage", "", "")
		rows, err := a.db.Query(r.Context(), `SELECT id,owner_id,name,visibility,positions,revision FROM mosaics WHERE tenant_id=$1 AND active AND (visibility='shared' OR owner_id=$2) AND ($3='' OR id=$3) AND id>$4 ORDER BY id LIMIT 201`, tenant, id.User.ID, mid, r.URL.Query().Get("cursor"))
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		items := []Mosaic{}
		cursor := ""
		count := 0
		for rows.Next() {
			var m Mosaic
			var raw []byte
			if rows.Scan(&m.ID, &m.Owner, &m.Name, &m.Visibility, &raw, &m.Revision) != nil || json.Unmarshal(raw, &m.Positions) != nil {
				dbFailure(w)
				return
			}
			count++
			if count > 200 {
				break
			}
			cursor = m.ID
			m.Editable = m.Owner == id.User.ID || (m.Visibility == "shared" && manage)
			items = append(items, m)
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		if count <= 200 {
			cursor = ""
		}
		if mid != "" {
			if len(items) == 0 {
				problem(w, 404, "mosaic_not_found")
				return
			}
			response(w, 200, items[0])
			return
		}
		response(w, 200, map[string]any{"items": items, "nextCursor": cursor})
		return
	}
	var input Mosaic
	if !body(w, r, &input) {
		return
	}
	if r.Method != "DELETE" && (!validName(input.Name) || (input.Visibility != "personal" && input.Visibility != "shared") || len(input.Positions) > 16) {
		problem(w, 422, "invalid_mosaic")
		return
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	manage := allowedWith(r.Context(), tx, current, tenant, "mosaics.manage", "", "")
	owner := current.User.ID
	if mid != "" {
		var visibility string
		var revision int64
		if tx.QueryRow(r.Context(), "SELECT owner_id,visibility,revision FROM mosaics WHERE tenant_id=$1 AND id=$2 AND active", tenant, mid).Scan(&owner, &visibility, &revision) != nil {
			problem(w, 404, "mosaic_not_found")
			return
		}
		if owner != current.User.ID && (visibility != "shared" || !manage) {
			problem(w, 404, "mosaic_not_found")
			return
		}
		if input.Revision != revision {
			problem(w, 412, "revision_conflict")
			return
		}
		// A manager can maintain a shared composition, but cannot privatize it
		// into the inactive creator's personal space.
		if owner != current.User.ID && input.Visibility == "personal" {
			problem(w, 403, "forbidden")
			return
		}
	}
	if input.Visibility == "shared" && !manage && mid == "" {
		problem(w, 403, "forbidden")
		return
	}
	if input.Visibility == "shared" && !manage && mid != "" {
		var old string
		tx.QueryRow(r.Context(), "SELECT visibility FROM mosaics WHERE id=$1", mid).Scan(&old)
		if old != "shared" {
			problem(w, 403, "forbidden")
			return
		}
	}
	for _, p := range input.Positions {
		if p.CameraID == "" {
			continue
		}
		var exists bool
		if tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM cameras WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)", tenant, p.CameraID).Scan(&exists) != nil || !exists {
			problem(w, 404, "camera_not_found")
			return
		}
		if (p.Profile != "" && !profileID.MatchString(p.Profile)) || len(p.Fallback) > 16 {
			problem(w, 422, "invalid_profile")
			return
		}
		for _, fallback := range p.Fallback {
			if !profileID.MatchString(fallback) {
				problem(w, 422, "invalid_profile")
				return
			}
		}
	}
	raw, _ := json.Marshal(input.Positions)
	if input.Positions == nil {
		raw = []byte("[]")
	}
	status := 200
	if r.Method == "POST" {
		mid = newID()
		input.Revision = 1
		status = 201
		_, err = tx.Exec(r.Context(), "INSERT INTO mosaics(id,tenant_id,owner_id,name,visibility,positions) VALUES($1,$2,$3,$4,$5,$6)", mid, tenant, owner, input.Name, input.Visibility, raw)
	} else if r.Method == "DELETE" {
		_, err = tx.Exec(r.Context(), "UPDATE mosaics SET active=false,revision=revision+1 WHERE id=$1", mid)
	} else {
		input.Revision++
		_, err = tx.Exec(r.Context(), "UPDATE mosaics SET name=$1,visibility=$2,positions=$3,revision=revision+1 WHERE id=$4", input.Name, input.Visibility, raw, mid)
	}
	if err != nil {
		dbFailure(w)
		return
	}
	if audit(r.Context(), tx, current, tenant, "mosaic."+strings.ToLower(r.Method), mid, map[string]string{"visibility": input.Visibility}) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	if r.Method == "DELETE" {
		w.WriteHeader(204)
		return
	}
	input.ID = mid
	input.Owner = owner
	input.Editable = true
	response(w, status, input)
}
func (a *App) qualityPolicy(w http.ResponseWriter, r *http.Request, id identity) {
	global := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	key := "installation.live"
	tenant := ""
	if global {
		if !id.User.GlobalAdmin {
			problem(w, 403, "forbidden")
			return
		}
	} else {
		var ok bool
		tenant, ok = a.tenantContext(w, r, id)
		if !ok {
			return
		}
		key = "tenant." + tenant + ".live"
	}
	if r.Method == "GET" {
		var rules []QualityRule
		revision := int64(0)
		inherited := false
		var raw []byte
		err := a.db.QueryRow(r.Context(), "SELECT value,revision FROM settings WHERE key=$1", key).Scan(&raw, &revision)
		if !global && (err == pgx.ErrNoRows || (err == nil && string(raw) == "null")) {
			inherited = true
			err = a.db.QueryRow(r.Context(), "SELECT value FROM settings WHERE key='installation.live'").Scan(&raw)
		}
		if err == pgx.ErrNoRows {
			rules = defaultQuality
		} else if err != nil || json.Unmarshal(raw, &rules) != nil {
			dbFailure(w)
			return
		}
		response(w, 200, map[string]any{"rules": rules, "revision": revision, "inherited": inherited})
		return
	}
	var in struct {
		Rules    []QualityRule `json:"rules"`
		Revision int64         `json:"revision"`
		Inherit  bool          `json:"inherit"`
	}
	if !body(w, r, &in) {
		return
	}
	next := 1
	if !(in.Inherit && !global) {
		for _, rule := range in.Rules {
			if rule.Min != next || rule.Max < rule.Min || rule.Max > 16 || !profileID.MatchString(rule.Profile) || len(rule.Fallback) > 16 {
				problem(w, 422, "invalid_quality_rules")
				return
			}
			for _, f := range rule.Fallback {
				if !profileID.MatchString(f) {
					problem(w, 422, "invalid_quality_rules")
					return
				}
			}
			next = rule.Max + 1
		}
		if next != 17 {
			problem(w, 422, "invalid_quality_rules")
			return
		}
	}
	tx, current, err := a.accessTransaction(r, id)
	if err != nil {
		dbFailure(w)
		return
	}
	defer tx.Rollback(r.Context())
	if (global && !current.User.GlobalAdmin) || (!global && !allowedWith(r.Context(), tx, current, tenant, "mosaics.manage", "", "")) {
		problem(w, 403, "forbidden")
		return
	}
	var revision int64
	err = tx.QueryRow(r.Context(), "SELECT revision FROM settings WHERE key=$1", key).Scan(&revision)
	if err != nil && err != pgx.ErrNoRows {
		dbFailure(w)
		return
	}
	if revision != in.Revision {
		problem(w, 412, "revision_conflict")
		return
	}
	raw, _ := json.Marshal(in.Rules)
	if in.Inherit && !global {
		// Keep a revisioned tombstone so stale edits cannot recreate an old override.
		raw = []byte("null")
	}
	_, err = tx.Exec(r.Context(), "INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=$2,revision=settings.revision+1", key, raw)
	if err != nil || audit(r.Context(), tx, current, tenant, "live.policy_changed", key, in) != nil || tx.Commit(r.Context()) != nil {
		dbFailure(w)
		return
	}
	response(w, 200, map[string]any{"rules": in.Rules, "revision": revision + 1, "inherited": in.Inherit})
}
func (a *App) registerMosaics(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST"} {
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/mosaics", a.protected(a.mosaics))
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/mosaics/{mosaic}", a.protected(a.mosaics))
	}
	for _, method := range []string{"GET", "PUT"} {
		mux.HandleFunc(method+" /api/v1/tenants/{tenant}/live-policy", a.protected(a.qualityPolicy))
		mux.HandleFunc(method+" /api/v1/admin/live-policy", a.protected(a.qualityPolicy))
	}
}
