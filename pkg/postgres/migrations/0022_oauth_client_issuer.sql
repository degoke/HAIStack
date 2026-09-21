-- Scope OAuth clients to the issuer that registered them.

ALTER TABLE hai_oauth_client ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE hai_oauth_client DROP CONSTRAINT hai_oauth_client_pkey;
ALTER TABLE hai_oauth_client ADD PRIMARY KEY (issuer, client_id);
