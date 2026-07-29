ALTER TABLE hai_blob_manifest ADD COLUMN IF NOT EXISTS encryption_algorithm TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN IF NOT EXISTS encryption_key_id TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN IF NOT EXISTS encryption_nonce TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN IF NOT EXISTS retention_mode TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN IF NOT EXISTS retain_until TIMESTAMPTZ;

ALTER TABLE hai_blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_algorithm TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_key_id TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN IF NOT EXISTS encryption_nonce TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN IF NOT EXISTS retention_mode TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN IF NOT EXISTS retain_until TIMESTAMPTZ;
