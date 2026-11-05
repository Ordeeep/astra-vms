-- Inserted with the business transaction. A single dispatcher assigns delivery
-- order AFTER commit; sequence/identity allocation alone is not commit order.
CREATE SEQUENCE IF NOT EXISTS event_delivery_sequence;
CREATE TABLE IF NOT EXISTS event_outbox (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 delivery bigint UNIQUE, tenant_id text NOT NULL, resource_id text NOT NULL,
 camera_id text NOT NULL DEFAULT '',site_id text NOT NULL DEFAULT '',
 revision bigint NOT NULL,kind text NOT NULL,payload jsonb NOT NULL DEFAULT '{}',
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 correlation_id text NOT NULL DEFAULT md5(random()::text||clock_timestamp()::text)
);
CREATE INDEX IF NOT EXISTS event_pending ON event_outbox(id) WHERE delivery IS NULL;
CREATE INDEX IF NOT EXISTS event_tenant_delivery ON event_outbox(tenant_id,delivery);
CREATE OR REPLACE FUNCTION publish_vms_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='cameras' THEN
  INSERT INTO event_outbox(tenant_id,resource_id,camera_id,site_id,revision,kind)
  VALUES(NEW.tenant_id,NEW.id,NEW.id,NEW.site_id,NEW.revision,'CameraStatusChanged');
 ELSIF TG_TABLE_NAME='recording_events' THEN
  INSERT INTO event_outbox(tenant_id,resource_id,camera_id,site_id,revision,kind,payload)
  VALUES(NEW.tenant_id,NEW.camera_id,NEW.camera_id,NEW.site_id,NEW.id,'CaptureStateChanged',jsonb_build_object('code',NEW.code));
  IF NEW.code IN ('storage_capacity','storage_unavailable') THEN
   INSERT INTO event_outbox(tenant_id,resource_id,revision,kind,payload)
   VALUES(NEW.tenant_id,NEW.tenant_id,NEW.id,'StoragePressure',jsonb_build_object('code',NEW.code));
  END IF;
 ELSIF TG_TABLE_NAME='archive_segments' THEN
  INSERT INTO event_outbox(tenant_id,resource_id,camera_id,site_id,revision,kind,payload)
  VALUES(NEW.tenant_id,NEW.id,NEW.camera_id,NEW.site_id,0,'ArchiveChanged',jsonb_build_object('available',NEW.available));
 ELSIF TG_TABLE_NAME='export_jobs' THEN
  IF TG_OP='INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
   INSERT INTO event_outbox(tenant_id,resource_id,revision,kind,payload)
   VALUES(NEW.tenant_id,NEW.id,0,'ExportStateChanged',jsonb_build_object('status',NEW.status));
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER camera_event AFTER INSERT OR UPDATE ON cameras
 FOR EACH ROW EXECUTE FUNCTION publish_vms_event();
CREATE OR REPLACE TRIGGER capture_event AFTER INSERT ON recording_events
 FOR EACH ROW EXECUTE FUNCTION publish_vms_event();
CREATE OR REPLACE TRIGGER archive_event AFTER INSERT OR UPDATE OF available ON archive_segments
 FOR EACH ROW EXECUTE FUNCTION publish_vms_event();
CREATE OR REPLACE TRIGGER export_event AFTER INSERT OR UPDATE OF status ON export_jobs
 FOR EACH ROW EXECUTE FUNCTION publish_vms_event();
