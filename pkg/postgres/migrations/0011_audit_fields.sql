-- Promote commonly queried audit fields from JSON details to indexed columns.

ALTER TABLE hai_audit_log ADD COLUMN IF NOT EXISTS subject TEXT;
ALTER TABLE hai_audit_log ADD COLUMN IF NOT EXISTS view_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN IF NOT EXISTS tool_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN IF NOT EXISTS module_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN IF NOT EXISTS blob_key TEXT;

CREATE INDEX IF NOT EXISTS idx_audit_log_action
    ON hai_audit_log (tenant_id, action, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_outcome
    ON hai_audit_log (tenant_id, outcome, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_view_name
    ON hai_audit_log (tenant_id, view_name, timestamp);
