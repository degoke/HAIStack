-- Subscription registry and delivery log for event-driven notifications.

CREATE TABLE IF NOT EXISTS subscription_registry (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    status        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    event_kind    TEXT NOT NULL,
    trigger_json  TEXT NOT NULL,
    channel_json  TEXT NOT NULL,
    retry_json    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscription_registry_match
    ON subscription_registry (status, resource_type, event_kind);

CREATE TABLE IF NOT EXISTS subscription_delivery_log (
    id              TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,
    event_sequence  INTEGER NOT NULL,
    attempt         INTEGER NOT NULL,
    status          TEXT NOT NULL,
    response_status INTEGER,
    response_body   TEXT,
    error_message   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (subscription_id, event_sequence, attempt)
);

CREATE INDEX IF NOT EXISTS idx_subscription_delivery_sub
    ON subscription_delivery_log (subscription_id, event_sequence);
