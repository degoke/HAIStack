ALTER TABLE hai_sync_inbox_applied ADD COLUMN IF NOT EXISTS ack_payload JSONB;

ALTER TABLE hai_event_log ADD COLUMN IF NOT EXISTS event_id TEXT;
ALTER TABLE hai_event_log ADD COLUMN IF NOT EXISTS origin_node_id TEXT;
ALTER TABLE hai_event_log ADD COLUMN IF NOT EXISTS local_version_id TEXT;

