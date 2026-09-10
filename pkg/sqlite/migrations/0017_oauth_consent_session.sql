CREATE TABLE IF NOT EXISTS hai_oauth_consent_session (
    session_id  TEXT PRIMARY KEY,
    issuer      TEXT NOT NULL,
    params      TEXT NOT NULL,
    csrf        TEXT NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    expires_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_oauth_consent_expires
    ON hai_oauth_consent_session (expires_at);
