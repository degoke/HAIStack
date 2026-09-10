ALTER TABLE hai_oauth_auth_code ADD COLUMN encounter TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS hai_oauth_launch_token (
    token           TEXT PRIMARY KEY,
    patient         TEXT NOT NULL DEFAULT '',
    encounter       TEXT NOT NULL DEFAULT '',
    user_id         TEXT NOT NULL DEFAULT '',
    tenant_hint     TEXT NOT NULL DEFAULT '',
    expires_at      TEXT NOT NULL,
    used            INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_oauth_launch_expires
    ON hai_oauth_launch_token (expires_at, used);
