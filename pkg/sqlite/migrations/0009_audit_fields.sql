-- Promote commonly queried audit fields from JSON details to indexed columns.

ALTER TABLE hai_audit_log ADD COLUMN tenant TEXT;
ALTER TABLE hai_audit_log ADD COLUMN subject TEXT;
ALTER TABLE hai_audit_log ADD COLUMN view_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN tool_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN module_name TEXT;
ALTER TABLE hai_audit_log ADD COLUMN blob_key TEXT;

CREATE INDEX IF NOT EXISTS idx_audit_log_action
    ON hai_audit_log (action, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_outcome
    ON hai_audit_log (outcome, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_view_name
    ON hai_audit_log (view_name, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_tenant
    ON hai_audit_log (tenant, timestamp);
