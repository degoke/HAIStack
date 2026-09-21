-- Scope OAuth clients to the issuer that registered them.

CREATE TABLE hai_oauth_client_new (
    issuer TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (issuer, client_id)
);
INSERT INTO hai_oauth_client_new (issuer, client_id, payload, updated_at)
SELECT '', client_id, payload, updated_at FROM hai_oauth_client;
DROP TABLE hai_oauth_client;
ALTER TABLE hai_oauth_client_new RENAME TO hai_oauth_client;
