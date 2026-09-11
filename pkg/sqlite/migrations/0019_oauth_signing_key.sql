CREATE TABLE IF NOT EXISTS hai_oauth_signing_key (
    issuer          TEXT PRIMARY KEY,
    key_id          TEXT NOT NULL,
    private_key_pem TEXT NOT NULL,
    created_at      TEXT NOT NULL
);
