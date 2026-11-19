package app

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Aggregate playable parts rather than loading segment indexes into the client.
// Authorization is applied BEFORE merging: inaccessible origins remain gaps.
const availableParts = `WITH segments AS (
 SELECT metadata FROM archive_segments
 WHERE tenant_id=$1 AND camera_id=$2 AND available
 AND ($3::boolean OR site_id=ANY($4::text[])) AND end_at>$5 AND start_at<$6
), parts AS (
 SELECT (p->>'start')::timestamptz AS start_at,(p->>'end')::timestamptz AS end_at
 FROM segments CROSS JOIN LATERAL jsonb_array_elements(coalesce(metadata->'parts','[]'::jsonb)) p
), playable AS (
 SELECT * FROM parts WHERE end_at>start_at AND end_at>$5 AND start_at<$6
)`

const continuousRecordings = `, preceding AS (
 SELECT *,max(end_at) OVER (ORDER BY start_at,end_at ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) AS previous_end FROM playable
), numbered AS (
 SELECT *,sum(CASE WHEN previous_end IS NULL OR start_at>previous_end+interval '1 microsecond' THEN 1 ELSE 0 END)
 OVER (ORDER BY start_at,end_at) AS block FROM preceding
), blocks AS (SELECT min(start_at) AS start_at,max(end_at) AS end_at FROM numbered GROUP BY block)`

type recordingRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (a *App) recordingAvailability(w http.ResponseWriter, r *http.Request, id identity) {
	tenant, ok := a.tenantContext(w, r, id)
	if !ok {
		return
	}
	camera := r.PathValue("camera")
	var site, zone string
	err := a.db.QueryRow(r.Context(), `SELECT c.site_id,s.timezone FROM cameras c JOIN sites s ON s.id=c.site_id AND s.tenant_id=c.tenant_id WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL`, tenant, camera).Scan(&site, &zone)
	if err == pgx.ErrNoRows {
		problem(w, 404, "camera_unavailable")
		return
	}
	if err != nil {
		dbFailure(w)
		return
	}
	grants, err := a.grants(r.Context(), id.User.ID, tenant)
	if err != nil {
		dbFailure(w)
		return
	}
	all := permits(grants, tenant, "playback", "", camera)
	sites := []string{}
	for _, grant := range grants {
		if grant.Action == "playback" && grant.Scope == "site" {
			sites = append(sites, grant.ResourceID)
		}
	}
	allowed := permits(grants, tenant, "playback", site, camera)
	if !allowed {
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM archive_segments WHERE tenant_id=$1 AND camera_id=$2 AND available AND ($3::boolean OR site_id=ANY($4::text[])))`, tenant, camera, all, sites).Scan(&allowed)
		if err != nil {
			dbFailure(w)
			return
		}
	}
	if !allowed {
		problem(w, 403, "forbidden")
		return
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		dbFailure(w)
		return
	}
	query := r.URL.Query()
	result := map[string]any{"timezone": zone}
	if month := query.Get("month"); month != "" {
		start, err := time.ParseInLocation("2006-01", month, location)
		if err != nil || len(month) != 7 {
			problem(w, 422, "invalid_recording_month")
			return
		}
		end := start.AddDate(0, 1, 0)
		rows, err := a.db.Query(r.Context(), availableParts+`
 SELECT to_char(d.day,'YYYY-MM-DD') FROM generate_series($7::date,($8::date-1)::timestamp,interval '1 day') AS d(day)
 WHERE EXISTS(SELECT 1 FROM playable p WHERE p.end_at>(d.day AT TIME ZONE $9) AND p.start_at<((d.day+interval '1 day') AT TIME ZONE $9)) ORDER BY d.day`, tenant, camera, all, sites, start, end, start.Format("2006-01-02"), end.Format("2006-01-02"), zone)
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		days := []string{}
		for rows.Next() {
			var day string
			if rows.Scan(&day) != nil {
				dbFailure(w)
				return
			}
			days = append(days, day)
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		result["days"] = days
	} else if direction := query.Get("direction"); direction != "" {
		at, err := time.Parse(time.RFC3339Nano, query.Get("at"))
		if err != nil || (direction != "previous" && direction != "next") {
			problem(w, 422, "invalid_recording_position")
			return
		}
		// Navigation is over continuous blocks, not files, and may cross days.
		predicate, order := "start_at>$7", "start_at"
		if direction == "previous" {
			predicate, order = "end_at<=$7", "end_at DESC"
		}
		var block recordingRange
		err = a.db.QueryRow(r.Context(), availableParts+continuousRecordings+` SELECT start_at,end_at FROM blocks WHERE `+predicate+` ORDER BY `+order+` LIMIT 1`, tenant, camera, all, sites, time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), at).Scan(&block.Start, &block.End)
		if err == pgx.ErrNoRows {
			result["interval"] = nil
		} else if err != nil {
			dbFailure(w)
			return
		} else {
			result["interval"] = block
		}
	} else if query.Has("start") || query.Has("end") {
		start, end, err := timeWindow(r, 48*time.Hour)
		if err != nil {
			problem(w, 422, "invalid_time_window")
			return
		}
		rows, err := a.db.Query(r.Context(), availableParts+continuousRecordings+` SELECT start_at,end_at FROM blocks ORDER BY start_at`, tenant, camera, all, sites, start, end)
		if err != nil {
			dbFailure(w)
			return
		}
		defer rows.Close()
		intervals := []recordingRange{}
		for rows.Next() {
			var interval recordingRange
			if rows.Scan(&interval.Start, &interval.End) != nil {
				dbFailure(w)
				return
			}
			intervals = append(intervals, interval)
		}
		if rows.Err() != nil {
			dbFailure(w)
			return
		}
		result["intervals"] = intervals
		if start.Before(time.Now()) && end.After(time.Now().Add(-5*time.Second)) {
			var delay float64
			err = a.db.QueryRow(r.Context(), `SELECT coalesce(max(greatest(extract(epoch FROM (now()-s.end_at)),extract(epoch FROM ((p->>'end')::timestamptz-(p->>'start')::timestamptz)))),0)
 FROM (SELECT end_at,metadata FROM archive_segments WHERE tenant_id=$1 AND camera_id=$2 AND available AND ($3::boolean OR site_id=ANY($4::text[])) ORDER BY end_at DESC LIMIT 1) s
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.metadata->'parts','[]'::jsonb)) p
 WHERE EXISTS(SELECT 1 FROM recording_status WHERE tenant_id=$1 AND camera_id=$2 AND code='recording' AND last_frame>now()-interval '5 seconds')`, tenant, camera, all, sites).Scan(&delay)
			if err != nil {
				dbFailure(w)
				return
			}
			result["sourceDelaySeconds"] = delay
		}
	}
	response(w, 200, result)
}
