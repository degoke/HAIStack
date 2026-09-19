-- OAuth authorization server state (JSON payloads mirror Postgres 0017_oauth.sql)

CREATE TABLE IF NOT EXISTS hai_oauth_client (
    client_id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS hai_oauth_auth_code (
    issuer TEXT NOT NULL,
    code TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, code)
);

CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_expires_idx ON hai_oauth_auth_code (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_issuer_expires_idx ON hai_oauth_auth_code (issuer, expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_refresh_token (
    issuer TEXT NOT NULL,
    token TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, token)
);

CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_expires_idx ON hai_oauth_refresh_token (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_issuer_expires_idx ON hai_oauth_refresh_token (issuer, expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_pending_auth (
    issuer TEXT NOT NULL,
    id TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, id)
);

CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_expires_idx ON hai_oauth_pending_auth (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_issuer_expires_idx ON hai_oauth_pending_auth (issuer, expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_replay_jti (
    jti TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_replay_jti_expires_idx ON hai_oauth_replay_jti (expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_revoked_jti (
    jti TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_revoked_jti_expires_idx ON hai_oauth_revoked_jti (expires_at);
