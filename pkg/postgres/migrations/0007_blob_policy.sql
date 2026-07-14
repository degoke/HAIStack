ALTER TABLE blob_manifest ADD COLUMN IF NOT EXISTS encryption_algorithm TEXT;
ALTER TABLE blob_manifest ADD COLUMN IF NOT EXISTS encryption_key_id TEXT;
ALTER TABLE blob_manifest ADD COLUMN IF NOT EXISTS encryption_nonce TEXT;
ALTER TABLE blob_manifest ADD COLUMN IF NOT EXISTS retention_mode TEXT;
ALTER TABLE blob_manifest ADD COLUMN IF NOT EXISTS retain_until TIMESTAMPTZ;

ALTER TABLE blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_algorithm TEXT;
ALTER TABLE blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_key_id TEXT;
ALTER TABLE blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_nonce TEXT;
ALTER TABLE blob_transfer_session ADD COLUMN IF NOT EXISTS retention_mode TEXT;
ALTER TABLE blob_transfer_session ADD COLUMN IF NOT EXISTS retain_until TIMESTAMPTZ;
