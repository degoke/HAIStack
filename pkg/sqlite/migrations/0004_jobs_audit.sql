-- SQLite background job queue and append-only audit log.

CREATE TABLE IF NOT EXISTS hai_background_job (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    payload    BLOB,
    status     TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    run_after  TEXT,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS idx_background_job_claim
    ON hai_background_job (type, status, run_after, created_at);

CREATE TABLE IF NOT EXISTS hai_audit_log (
    id            TEXT PRIMARY KEY,
    timestamp     TEXT NOT NULL,
    actor         TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT,
    details       TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_log_resource
    ON hai_audit_log (resource_type, resource_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_log_actor
    ON hai_audit_log (actor, timestamp);
