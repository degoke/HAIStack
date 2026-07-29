-- haistack-postgres initial schema

CREATE TABLE IF NOT EXISTS hai_tenant (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hai_resource (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    resource_type TEXT NOT NULL,
    id            TEXT NOT NULL,
    version_id    TEXT NOT NULL,
    last_updated  TIMESTAMPTZ NOT NULL,
    json          JSONB NOT NULL,
    hash          TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, resource_type, id)
);

CREATE TABLE IF NOT EXISTS hai_resource_history (
    rowid         BIGSERIAL PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    version_id    TEXT NOT NULL,
    action        TEXT NOT NULL,
    timestamp     TIMESTAMPTZ NOT NULL,
    hash          TEXT,
    deleted       BOOLEAN NOT NULL DEFAULT false,
    json          JSONB,
    UNIQUE (tenant_id, resource_type, resource_id, version_id)
);

CREATE INDEX IF NOT EXISTS idx_resource_history_resource
    ON hai_resource_history (tenant_id, resource_type, resource_id, rowid);

CREATE TABLE IF NOT EXISTS hai_event_log (
    sequence      BIGSERIAL PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    version_id    TEXT NOT NULL,
    action        TEXT NOT NULL,
    timestamp     TIMESTAMPTZ NOT NULL,
    hash          TEXT
);

CREATE TABLE IF NOT EXISTS hai_resource_id_registry (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    resource_type TEXT NOT NULL,
    id            TEXT NOT NULL,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, resource_type, id)
);

CREATE TABLE IF NOT EXISTS hai_node_registry (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    node_id       TEXT NOT NULL,
    metadata      JSONB,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, node_id)
);

CREATE TABLE IF NOT EXISTS hai_sync_conflict (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES hai_tenant (id),
    resource_type     TEXT NOT NULL,
    resource_id       TEXT NOT NULL,
    local_version_id  TEXT NOT NULL,
    remote_version_id TEXT NOT NULL,
    reason            TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    resolved_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sync_conflict_resource
    ON hai_sync_conflict (tenant_id, resource_type, resource_id);

CREATE TABLE IF NOT EXISTS hai_search_token (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_token_lookup
    ON hai_search_token (tenant_id, field_key, value);

CREATE TABLE IF NOT EXISTS hai_search_string (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_string_lookup
    ON hai_search_string (tenant_id, field_key, value);

CREATE TABLE IF NOT EXISTS hai_search_date (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_date_lookup
    ON hai_search_date (tenant_id, field_key, value);

CREATE TABLE IF NOT EXISTS hai_search_number (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_number_lookup
    ON hai_search_number (tenant_id, field_key, value);

CREATE TABLE IF NOT EXISTS hai_search_reference (
    tenant_id     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    field_key     TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, resource_type, resource_id, field_key, value)
);

CREATE INDEX IF NOT EXISTS idx_search_reference_lookup
    ON hai_search_reference (tenant_id, field_key, value);

CREATE TABLE IF NOT EXISTS hai_sync_cursor (
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    position   TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS hai_binary_object (
    tenant_id    TEXT NOT NULL,
    key          TEXT NOT NULL,
    content_type TEXT,
    size         BIGINT NOT NULL,
    hash         TEXT,
    data         BYTEA,
    location     TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);

CREATE TABLE IF NOT EXISTS hai_audit_log (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    timestamp     TIMESTAMPTZ NOT NULL,
    actor         TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT,
    details       JSONB
);

CREATE INDEX IF NOT EXISTS idx_audit_log_resource
    ON hai_audit_log (tenant_id, resource_type, resource_id, timestamp);

CREATE TABLE IF NOT EXISTS hai_module_registry (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,
    metadata      JSONB,
    registered_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS hai_materialized_view (
    tenant_id  TEXT NOT NULL,
    view_name  TEXT NOT NULL,
    key        TEXT NOT NULL,
    payload    BYTEA,
    version    BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, view_name, key)
);

CREATE TABLE IF NOT EXISTS hai_analytics_event (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    timestamp  TIMESTAMPTZ NOT NULL,
    dimensions JSONB,
    values     JSONB,
    payload    BYTEA
);

CREATE INDEX IF NOT EXISTS idx_analytics_event_name_time
    ON hai_analytics_event (tenant_id, name, timestamp);

CREATE TABLE IF NOT EXISTS hai_background_job (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    type       TEXT NOT NULL,
    payload    BYTEA,
    status     TEXT NOT NULL,
    attempts   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    run_after  TIMESTAMPTZ,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS idx_background_job_claim
    ON hai_background_job (tenant_id, type, status, run_after, created_at);
