CREATE TABLE IF NOT EXISTS hai_auth_principal (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    display_name TEXT,
    attributes   JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hai_auth_role (
    name       TEXT PRIMARY KEY,
    permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hai_auth_tenant_binding (
    tenant_id    TEXT NOT NULL REFERENCES hai_tenant (id),
    principal_id TEXT NOT NULL REFERENCES hai_auth_principal (id) ON DELETE CASCADE,
    roles        JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, principal_id)
);

CREATE INDEX IF NOT EXISTS idx_auth_tenant_binding_principal
    ON hai_auth_tenant_binding (principal_id, tenant_id);

CREATE TABLE IF NOT EXISTS hai_auth_device_identity (
    tenant_id            TEXT NOT NULL REFERENCES hai_tenant (id),
    device_id            TEXT NOT NULL,
    status               TEXT NOT NULL,
    trusted              BOOLEAN NOT NULL DEFAULT false,
    metadata             JSONB,
    linked_principal_id  TEXT REFERENCES hai_auth_principal (id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_auth_device_linked_principal
    ON hai_auth_device_identity (linked_principal_id);

CREATE TABLE IF NOT EXISTS hai_auth_policy_document (
    tenant_id   TEXT NOT NULL REFERENCES hai_tenant (id),
    name        TEXT NOT NULL,
    format      TEXT NOT NULL,
    version     TEXT NOT NULL,
    body        BYTEA NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_auth_policy_active
    ON hai_auth_policy_document (tenant_id, active, updated_at DESC);
