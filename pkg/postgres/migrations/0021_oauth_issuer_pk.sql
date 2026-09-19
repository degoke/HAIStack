-- Composite primary keys so two issuers can hold the same code/token/session id.
-- Idempotent: drop existing primary keys, then recreate as (issuer, id).

ALTER TABLE hai_oauth_auth_code DROP CONSTRAINT IF EXISTS hai_oauth_auth_code_pkey;
ALTER TABLE hai_oauth_auth_code ADD PRIMARY KEY (issuer, code);

ALTER TABLE hai_oauth_refresh_token DROP CONSTRAINT IF EXISTS hai_oauth_refresh_token_pkey;
ALTER TABLE hai_oauth_refresh_token ADD PRIMARY KEY (issuer, token);

ALTER TABLE hai_oauth_pending_auth DROP CONSTRAINT IF EXISTS hai_oauth_pending_auth_pkey;
ALTER TABLE hai_oauth_pending_auth ADD PRIMARY KEY (issuer, id);
