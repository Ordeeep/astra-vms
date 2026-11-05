CREATE TABLE IF NOT EXISTS operators (
  id text PRIMARY KEY,
  username text NOT NULL,
  password_hash text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  global_admin boolean NOT NULL DEFAULT false,
  revision bigint NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS operator_username ON operators(lower(username));
CREATE TABLE IF NOT EXISTS tenants (
  id text PRIMARY KEY,
  name text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS memberships (
  tenant_id text NOT NULL REFERENCES tenants(id),
  operator_id text NOT NULL REFERENCES operators(id),
  PRIMARY KEY(tenant_id,operator_id)
);
CREATE TABLE IF NOT EXISTS permission_groups (
  id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(id),
  name text NOT NULL,
  grants jsonb NOT NULL DEFAULT '[]',
  revision bigint NOT NULL DEFAULT 1,
  UNIQUE(tenant_id,id)
);
ALTER TABLE memberships ADD COLUMN IF NOT EXISTS revision bigint NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS group_members (
  tenant_id text NOT NULL,
  group_id text NOT NULL,
  operator_id text NOT NULL,
  PRIMARY KEY(tenant_id,group_id,operator_id),
  FOREIGN KEY(tenant_id,group_id) REFERENCES permission_groups(tenant_id,id),
  FOREIGN KEY(tenant_id,operator_id) REFERENCES memberships(tenant_id,operator_id)
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash text PRIMARY KEY,
  operator_id text NOT NULL REFERENCES operators(id),
  csrf text NOT NULL,
  expires timestamptz NOT NULL,
  created timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS view_contexts (
  id text PRIMARY KEY,
  session_hash text NOT NULL REFERENCES sessions(token_hash) ON DELETE CASCADE,
  tenant_id text NOT NULL REFERENCES tenants(id),
  created timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_id text NOT NULL,
  tenant_id text,
  action text NOT NULL,
  resource_id text,
  details jsonb NOT NULL DEFAULT '{}',
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_tenant_time ON audit_events(tenant_id,occurred_at DESC);
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS revision bigint;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS outcome text NOT NULL DEFAULT 'succeeded';
CREATE TABLE IF NOT EXISTS sites (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id),
 name text NOT NULL, address text NOT NULL DEFAULT '', timezone text NOT NULL,
 active boolean NOT NULL DEFAULT true, revision bigint NOT NULL DEFAULT 1,
 UNIQUE(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS cameras (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id),
 site_id text NOT NULL, name text NOT NULL, active boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1, source_version text NOT NULL,
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,site_id) REFERENCES sites(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS camera_sources (
 id text PRIMARY KEY, tenant_id text NOT NULL, camera_id text NOT NULL,
 profiles jsonb NOT NULL, secrets bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,camera_id,id),
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS camera_locations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,camera_id text NOT NULL,site_id text NOT NULL,
 effective_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id),
 FOREIGN KEY(tenant_id,site_id) REFERENCES sites(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS camera_tenant_site ON cameras(tenant_id,site_id);
CREATE TABLE IF NOT EXISTS jobs (
 id text PRIMARY KEY,tenant_id text NOT NULL REFERENCES tenants(id),actor_id text NOT NULL REFERENCES operators(id),
 camera_id text NOT NULL,kind text NOT NULL,status text NOT NULL,
 result jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),finished_at timestamptz,
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS live_sessions (
 id text PRIMARY KEY,tenant_id text NOT NULL,context_id text NOT NULL,session_hash text NOT NULL,
 camera_id text NOT NULL,source_version text NOT NULL,profile_id text NOT NULL,
 worker_token text NOT NULL,media_path text NOT NULL,native_url text NOT NULL DEFAULT '',
 expires timestamptz NOT NULL,
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS live_expiry ON live_sessions(expires);
CREATE TABLE IF NOT EXISTS mosaics (
 id text PRIMARY KEY,tenant_id text NOT NULL REFERENCES tenants(id),owner_id text NOT NULL REFERENCES operators(id),
 name text NOT NULL,visibility text NOT NULL CHECK(visibility IN ('personal','shared')),
 positions jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,active boolean NOT NULL DEFAULT true
);
CREATE TABLE IF NOT EXISTS settings (
 key text PRIMARY KEY,value jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS recording_policies (
 tenant_id text PRIMARY KEY REFERENCES tenants(id),
 quota_bytes bigint NOT NULL CHECK(quota_bytes>=0),
 export_bytes bigint NOT NULL CHECK(export_bytes>=0),
 retention_days integer NOT NULL CHECK(retention_days BETWEEN 1 AND 3650),
 export_hours integer NOT NULL DEFAULT 24 CHECK(export_hours BETWEEN 1 AND 720),
 revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS recording_configs (
 tenant_id text NOT NULL,camera_id text NOT NULL,
 enabled boolean NOT NULL DEFAULT false,profile text NOT NULL DEFAULT 'main',
 schedule jsonb NOT NULL DEFAULT '[]',always_on boolean NOT NULL DEFAULT true,
 retention_days integer,revision bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,camera_id),
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id),
 CHECK(retention_days IS NULL OR retention_days BETWEEN 1 AND 3650)
);
CREATE TABLE IF NOT EXISTS archive_segments (
 id text PRIMARY KEY,tenant_id text NOT NULL,camera_id text NOT NULL,
 site_id text NOT NULL,source_version text NOT NULL,profile_id text NOT NULL,
 start_at timestamptz NOT NULL,end_at timestamptz NOT NULL,
 bytes bigint NOT NULL,charged bigint NOT NULL,metadata jsonb NOT NULL,
 available boolean NOT NULL DEFAULT true,discard_reason text,
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id),
 FOREIGN KEY(tenant_id,site_id) REFERENCES sites(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS archive_camera_time ON archive_segments(tenant_id,camera_id,start_at) WHERE available;
CREATE INDEX IF NOT EXISTS archive_expiry ON archive_segments(end_at) WHERE available;
CREATE TABLE IF NOT EXISTS recording_status (
 tenant_id text NOT NULL,camera_id text NOT NULL,code text NOT NULL,
 last_frame timestamptz,last_write timestamptz,updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,camera_id),FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS playback_sessions (
 id text PRIMARY KEY,tenant_id text NOT NULL,camera_id text NOT NULL,
 context_id text NOT NULL,session_hash text NOT NULL,
 start_at timestamptz NOT NULL,end_at timestamptz NOT NULL,expires timestamptz NOT NULL,
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS playback_expiry ON playback_sessions(expires);
CREATE TABLE IF NOT EXISTS export_previews (
 id text PRIMARY KEY,tenant_id text NOT NULL REFERENCES tenants(id),actor_id text NOT NULL REFERENCES operators(id),
 plan jsonb NOT NULL,estimated_bytes bigint NOT NULL,expires timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS export_jobs (
 id text PRIMARY KEY,tenant_id text NOT NULL REFERENCES tenants(id),actor_id text NOT NULL REFERENCES operators(id),
 preview_id text NOT NULL,idempotency_key text NOT NULL,plan jsonb NOT NULL,
 status text NOT NULL,code text NOT NULL DEFAULT '',result jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(),expires timestamptz NOT NULL,
 UNIQUE(tenant_id,actor_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS export_queue ON export_jobs(created_at) WHERE status IN ('queued','running');
ALTER TABLE export_jobs ADD COLUMN IF NOT EXISTS artifacts_cleaned boolean NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS quota_jobs (
 id text PRIMARY KEY,tenant_id text NOT NULL REFERENCES tenants(id),actor_id text NOT NULL REFERENCES operators(id),
 target_bytes bigint NOT NULL CHECK(target_bytes>=0),policy_revision bigint NOT NULL,
 status text NOT NULL DEFAULT 'preview',code text NOT NULL DEFAULT '',result jsonb NOT NULL DEFAULT '{}',
 idempotency_key text,created_at timestamptz NOT NULL DEFAULT now(),expires timestamptz NOT NULL DEFAULT now()+interval '30 minutes',
 UNIQUE(tenant_id,actor_id,idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS quota_job_active ON quota_jobs(tenant_id) WHERE status IN ('queued','applying','waiting');
ALTER TABLE quota_jobs ADD COLUMN IF NOT EXISTS last_attempt timestamptz NOT NULL DEFAULT '-infinity';
CREATE TABLE IF NOT EXISTS recording_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,camera_id text NOT NULL,site_id text NOT NULL,source_version text NOT NULL,
 code text NOT NULL,occurred_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,camera_id) REFERENCES cameras(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS recording_event_cursor ON recording_events(tenant_id,id DESC);
