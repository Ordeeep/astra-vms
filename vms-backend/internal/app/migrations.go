package app

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ContractMajor = 1

// The initial migration also adopts pre-release installations: all operations
// are additive and bootstrap never resets an existing account or credential.
// Once released, extend this list; never edit a previously released migration.
func migrate(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, "SELECT coalesce(max(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	migrations := []string{schemaSQL + "\n" + eventsSQL, `
		ALTER TABLE operators ADD COLUMN name text NOT NULL DEFAULT '';
		ALTER TABLE operators ADD COLUMN email text NOT NULL DEFAULT '';
		UPDATE operators SET name=username;
	`, `
		CREATE INDEX operator_email_login ON operators(lower(email)) WHERE email<>'';
	`, `
 ALTER TABLE camera_sources ADD COLUMN connection_type text NOT NULL DEFAULT 'rtsp';
 ALTER TABLE camera_sources ADD COLUMN connection jsonb NOT NULL DEFAULT 'null';
 ALTER TABLE camera_sources ADD COLUMN connection_revision bigint NOT NULL DEFAULT 1;
 `, `
 ALTER TABLE cameras ADD COLUMN deleted_at timestamptz;
 CREATE TABLE camera_deletions (
  tenant_id text NOT NULL, camera_id text NOT NULL, actor_id text NOT NULL,
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed')),
  cursor text NOT NULL DEFAULT '', attempts integer NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '', next_attempt timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
  PRIMARY KEY(tenant_id,camera_id),
  FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
 );
 CREATE INDEX camera_deletion_pending ON camera_deletions(next_attempt) WHERE status='pending';
 -- Serialize new consumers against deletion, including requests that read the
 -- camera before a worker round-trip. A tombstone must never get a new session.
 CREATE FUNCTION require_operational_camera() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  PERFORM 1 FROM cameras WHERE tenant_id=NEW.tenant_id AND id=NEW.camera_id AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'camera unavailable' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER live_operational_camera BEFORE INSERT ON live_sessions FOR EACH ROW EXECUTE FUNCTION require_operational_camera();
 CREATE TRIGGER playback_operational_camera BEFORE INSERT ON playback_sessions FOR EACH ROW EXECUTE FUNCTION require_operational_camera();
 `}
	if version > len(migrations) {
		return errors.New("database requires a newer VMS core")
	}
	for i := version; i < len(migrations); i++ {
		if _, err = tx.Exec(ctx, migrations[i]); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", i+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
