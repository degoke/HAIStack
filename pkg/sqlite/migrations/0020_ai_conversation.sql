-- Agent harness conversation transcripts (tenant-scoped).

CREATE TABLE IF NOT EXISTS hai_ai_conversation (
    id            TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    actor         TEXT,
    subject       TEXT,
    messages_json TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_ai_conversation_actor
    ON hai_ai_conversation (tenant_id, actor, updated_at);
