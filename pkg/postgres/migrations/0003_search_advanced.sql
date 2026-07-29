CREATE TABLE IF NOT EXISTS hai_search_text (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL DEFAULT 'document',
    document      TEXT NOT NULL,
    tsvector      tsvector GENERATED ALWAYS AS (to_tsvector('english', document)) STORED,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key)
);

CREATE INDEX IF NOT EXISTS idx_search_text_fts
    ON hai_search_text USING GIN (tsvector);

CREATE TABLE IF NOT EXISTS hai_search_composite (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_composite_lookup
    ON hai_search_composite (tenant_id, resource_type, field_key, value);
