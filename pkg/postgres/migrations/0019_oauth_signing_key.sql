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
