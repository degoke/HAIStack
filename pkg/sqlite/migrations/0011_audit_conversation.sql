ALTER TABLE hai_audit_log ADD COLUMN conversation_id TEXT;

CREATE INDEX IF NOT EXISTS idx_audit_log_conversation
    ON hai_audit_log (conversation_id, timestamp);
