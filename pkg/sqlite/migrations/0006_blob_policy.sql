ALTER TABLE hai_blob_manifest ADD COLUMN encryption_algorithm TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN encryption_key_id TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN encryption_nonce TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN retention_mode TEXT;
ALTER TABLE hai_blob_manifest ADD COLUMN retain_until TEXT;

ALTER TABLE hai_blob_transfer_session ADD COLUMN encryption_algorithm TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN encryption_key_id TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN encryption_nonce TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN retention_mode TEXT;
ALTER TABLE hai_blob_transfer_session ADD COLUMN retain_until TEXT;
