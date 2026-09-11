CREATE TABLE IF NOT EXISTS hai_oauth_signing_key_v2 (
    issuer           TEXT NOT NULL,
    key_id           TEXT NOT NULL,
    private_key_pem  TEXT NOT NULL,
    encryption_nonce TEXT NOT NULL DEFAULT '',
    active           INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    retired_at       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (issuer, key_id)
);

INSERT INTO hai_oauth_signing_key_v2 (issuer, key_id, private_key_pem, active, created_at)
SELECT issuer, key_id, private_key_pem, 1, created_at FROM hai_oauth_signing_key;

DROP TABLE hai_oauth_signing_key;

ALTER TABLE hai_oauth_signing_key_v2 RENAME TO hai_oauth_signing_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_signing_key_active
    ON hai_oauth_signing_key (issuer)
    WHERE active = 1 AND retired_at = '';
