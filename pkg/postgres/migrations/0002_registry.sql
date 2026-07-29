-- haistack-postgres registry catalog schema
--
-- hai_definition_resource and hai_definition_target are global (shared base catalog).
-- hai_registry_install is hai_tenant-scoped enablement/install overlay.

CREATE TABLE IF NOT EXISTS hai_definition_resource (
    canonical_url      TEXT NOT NULL,
    version            TEXT NOT NULL,
    fhir_version       TEXT NOT NULL,
    fhir_resource_type TEXT NOT NULL,
    definition_kind    TEXT NOT NULL,
    name               TEXT NOT NULL,
    status             TEXT NOT NULL,
    package_name       TEXT NOT NULL DEFAULT '',
    package_version    TEXT NOT NULL DEFAULT '',
    module_name        TEXT NOT NULL DEFAULT '',
    json_data          JSONB NOT NULL,
    installed_at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (canonical_url, version)
);

CREATE INDEX IF NOT EXISTS idx_definition_resource_kind
    ON hai_definition_resource (definition_kind, fhir_version);

CREATE TABLE IF NOT EXISTS hai_definition_target (
    canonical_url        TEXT NOT NULL,
    version              TEXT NOT NULL,
    target_resource_type TEXT NOT NULL,
    target_role          TEXT NOT NULL,
    PRIMARY KEY (canonical_url, version, target_resource_type, target_role),
    FOREIGN KEY (canonical_url, version)
        REFERENCES hai_definition_resource (canonical_url, version) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_definition_target_lookup
    ON hai_definition_target (target_resource_type, target_role);

CREATE TABLE IF NOT EXISTS hai_registry_install (
    tenant_id            TEXT NOT NULL REFERENCES hai_tenant (id),
    definition_kind      TEXT NOT NULL,
    canonical_url        TEXT NOT NULL,
    version              TEXT NOT NULL,
    target_resource_type TEXT NOT NULL,
    enabled              BOOLEAN NOT NULL DEFAULT true,
    source_module        TEXT NOT NULL DEFAULT '',
    installed_at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, definition_kind, canonical_url, version, target_resource_type)
);

CREATE INDEX IF NOT EXISTS idx_registry_install_target
    ON hai_registry_install (tenant_id, target_resource_type, definition_kind, enabled);
