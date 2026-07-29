-- Subscription registry and delivery log for event-driven notifications.

CREATE TABLE IF NOT EXISTS hai_subscription_registry (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    status        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    event_kind    TEXT NOT NULL,
    trigger_json  JSONB NOT NULL,
    channel_json  JSONB NOT NULL,
    retry_json    JSONB,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscription_registry_match
    ON hai_subscription_registry (tenant_id, status, resource_type, event_kind);

CREATE TABLE IF NOT EXISTS hai_subscription_delivery_log (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    event_sequence  BIGINT NOT NULL,
    attempt         INTEGER NOT NULL,
    status          TEXT NOT NULL,
    response_status INTEGER,
    response_body   TEXT,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, subscription_id, event_sequence, attempt)
);

CREATE INDEX IF NOT EXISTS idx_subscription_delivery_sub
    ON hai_subscription_delivery_log (tenant_id, subscription_id, event_sequence);
