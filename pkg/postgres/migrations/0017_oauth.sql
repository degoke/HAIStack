CREATE TABLE IF NOT EXISTS hai_oauth_client (
    client_id TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hai_oauth_auth_code (
    code TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_expires_idx ON hai_oauth_auth_code (expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_refresh_token (
    token TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_expires_idx ON hai_oauth_refresh_token (expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_pending_auth (
    id TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_expires_idx ON hai_oauth_pending_auth (expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_replay_jti (
    jti TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_replay_jti_expires_idx ON hai_oauth_replay_jti (expires_at);

CREATE TABLE IF NOT EXISTS hai_oauth_revoked_jti (
    jti TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS hai_oauth_revoked_jti_expires_idx ON hai_oauth_revoked_jti (expires_at);
