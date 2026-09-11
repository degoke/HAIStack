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
