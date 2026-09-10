CREATE TABLE IF NOT EXISTS hai_oauth_auth_code (
    code                    TEXT PRIMARY KEY,
    client_id               TEXT NOT NULL,
    redirect_uri            TEXT NOT NULL,
    scope                   TEXT NOT NULL DEFAULT '',
    code_challenge          TEXT NOT NULL DEFAULT '',
    code_challenge_method   TEXT NOT NULL DEFAULT '',
    state                   TEXT NOT NULL DEFAULT '',
    patient                 TEXT NOT NULL DEFAULT '',
    user_id                 TEXT NOT NULL DEFAULT '',
    tenant_hint             TEXT NOT NULL DEFAULT '',
    expires_at              TEXT NOT NULL,
    used                    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_oauth_auth_code_expires
    ON hai_oauth_auth_code (expires_at, used);

CREATE TABLE IF NOT EXISTS hai_oauth_refresh_token (
    token           TEXT PRIMARY KEY,
    client_id       TEXT NOT NULL,
    scope           TEXT NOT NULL DEFAULT '',
    subject         TEXT NOT NULL DEFAULT '',
    patient         TEXT NOT NULL DEFAULT '',
    encounter       TEXT NOT NULL DEFAULT '',
    fhir_user       TEXT NOT NULL DEFAULT '',
    tenant_hint     TEXT NOT NULL DEFAULT '',
    expires_at      TEXT NOT NULL,
    revoked         INTEGER NOT NULL DEFAULT 0,
    replaced_by     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_oauth_refresh_client
    ON hai_oauth_refresh_token (client_id, revoked, expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_revoked_token (
    token_id    TEXT PRIMARY KEY,
    token_type  TEXT NOT NULL,
    expires_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_oauth_revoked_expires
    ON hai_oauth_revoked_token (expires_at);
