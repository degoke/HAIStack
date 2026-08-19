-- Preserve the exact JSON bytes supplied for immutable resource history.
-- Existing installations may have JSONB here; converting with ::text keeps
-- the stored value readable while ensuring future writes do not reformat it.
ALTER TABLE hai_resource_history
    ALTER COLUMN json TYPE TEXT USING json::text;
