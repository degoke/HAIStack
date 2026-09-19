-- Bind OAuth ephemeral rows to the issuer URL that created them (multi-tenant shared store).
-- Idempotent: greenfield 0014 already has issuer; upgrades from older 0014 add the column.

ALTER TABLE hai_oauth_auth_code ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE hai_oauth_refresh_token ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE hai_oauth_pending_auth ADD COLUMN IF NOT EXISTS issuer TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS hai_oauth_auth_code_issuer_expires_idx ON hai_oauth_auth_code (issuer, expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_refresh_token_issuer_expires_idx ON hai_oauth_refresh_token (issuer, expires_at);
CREATE INDEX IF NOT EXISTS hai_oauth_pending_auth_issuer_expires_idx ON hai_oauth_pending_auth (issuer, expires_at);
