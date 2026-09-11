CREATE TABLE IF NOT EXISTS hai_oauth_auth_code (
    code                    TEXT PRIMARY KEY,
    client_id               TEXT NOT NULL,
    redirect_uri            TEXT NOT NULL,
    scope                   TEXT NOT NULL DEFAULT '',
    code_challenge          TEXT NOT NULL DEFAULT '',
    code_challenge_method   TEXT NOT NULL DEFAULT '',
    state                   TEXT NOT NULL DEFAULT '',
    patient                 TEXT NOT NULL DEFAULT '',
    encounter               TEXT NOT NULL DEFAULT '',
    user_id                 TEXT NOT NULL DEFAULT '',
    tenant_hint             TEXT NOT NULL DEFAULT '',
    issuer                  TEXT NOT NULL DEFAULT '',
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
    issuer          TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS hai_oauth_launch_token (
    token           TEXT PRIMARY KEY,
    patient         TEXT NOT NULL DEFAULT '',
    encounter       TEXT NOT NULL DEFAULT '',
    user_id         TEXT NOT NULL DEFAULT '',
    tenant_hint     TEXT NOT NULL DEFAULT '',
    issuer          TEXT NOT NULL DEFAULT '',
    expires_at      TEXT NOT NULL,
    used            INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_oauth_launch_expires
    ON hai_oauth_launch_token (expires_at, used);

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

CREATE TABLE IF NOT EXISTS hai_oauth_client (
    client_id       TEXT NOT NULL,
    issuer          TEXT NOT NULL,
    client_json     TEXT NOT NULL,
    client_secret   TEXT NOT NULL DEFAULT '',
    confidential    INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (client_id, issuer)
);

CREATE INDEX IF NOT EXISTS idx_oauth_client_issuer
    ON hai_oauth_client (issuer);

CREATE TABLE IF NOT EXISTS hai_oauth_signing_key (
    issuer           TEXT NOT NULL,
    key_id           TEXT NOT NULL,
    private_key_pem  TEXT NOT NULL,
    encryption_nonce TEXT NOT NULL DEFAULT '',
    active           INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    retired_at       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (issuer, key_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_signing_key_active
    ON hai_oauth_signing_key (issuer)
    WHERE active = 1 AND retired_at = '';

CREATE TABLE IF NOT EXISTS hai_oauth_rate_limit (
    bucket_key     TEXT NOT NULL,
    endpoint       TEXT NOT NULL,
    window_start   TEXT NOT NULL,
    request_count  INTEGER NOT NULL,
    PRIMARY KEY (bucket_key, endpoint)
);
