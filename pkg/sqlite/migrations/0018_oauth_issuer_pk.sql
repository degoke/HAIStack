-- Composite primary keys so two issuers can hold the same code/token/session id.

CREATE TABLE hai_oauth_auth_code_new (
    issuer TEXT NOT NULL,
    code TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, code)
);
INSERT INTO hai_oauth_auth_code_new (issuer, code, payload, expires_at)
SELECT issuer, code, payload, expires_at FROM hai_oauth_auth_code;
DROP TABLE hai_oauth_auth_code;
ALTER TABLE hai_oauth_auth_code_new RENAME TO hai_oauth_auth_code;
CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_expires_idx ON hai_oauth_auth_code (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_issuer_expires_idx ON hai_oauth_auth_code (issuer, expires_at);

CREATE TABLE hai_oauth_refresh_token_new (
    issuer TEXT NOT NULL,
    token TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, token)
);
INSERT INTO hai_oauth_refresh_token_new (issuer, token, payload, expires_at)
SELECT issuer, token, payload, expires_at FROM hai_oauth_refresh_token;
DROP TABLE hai_oauth_refresh_token;
ALTER TABLE hai_oauth_refresh_token_new RENAME TO hai_oauth_refresh_token;
CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_expires_idx ON hai_oauth_refresh_token (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_issuer_expires_idx ON hai_oauth_refresh_token (issuer, expires_at);

CREATE TABLE hai_oauth_pending_auth_new (
    issuer TEXT NOT NULL,
    id TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (issuer, id)
);
INSERT INTO hai_oauth_pending_auth_new (issuer, id, payload, expires_at)
SELECT issuer, id, payload, expires_at FROM hai_oauth_pending_auth;
DROP TABLE hai_oauth_pending_auth;
ALTER TABLE hai_oauth_pending_auth_new RENAME TO hai_oauth_pending_auth;
CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_expires_idx ON hai_oauth_pending_auth (expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_issuer_expires_idx ON hai_oauth_pending_auth (issuer, expires_at);
