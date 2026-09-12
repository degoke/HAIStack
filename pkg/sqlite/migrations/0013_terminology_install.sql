CREATE TABLE IF NOT EXISTS hai_terminology_install (
    tenant_id text NOT NULL,
    pack_name text NOT NULL DEFAULT '',
    pack_version text NOT NULL DEFAULT '',
    resource_type text NOT NULL DEFAULT 'CodeSystem',
    canonical_url text NOT NULL,
    version text NOT NULL DEFAULT '',
    enabled integer NOT NULL DEFAULT 1,
    source_module text NOT NULL DEFAULT '',
    installed_at text NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, resource_type, canonical_url, version)
);

CREATE INDEX IF NOT EXISTS hai_terminology_install_pack_idx
    ON hai_terminology_install (tenant_id, pack_name, pack_version);
