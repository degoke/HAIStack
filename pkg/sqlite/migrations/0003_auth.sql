CREATE TABLE IF NOT EXISTS auth_principal (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    display_name TEXT,
    attributes   TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS auth_role (
    name        TEXT PRIMARY KEY,
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS auth_tenant_binding (
    tenant_id    TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    roles        TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (tenant_id, principal_id),
    FOREIGN KEY (principal_id) REFERENCES auth_principal (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_auth_tenant_binding_principal
    ON auth_tenant_binding (principal_id, tenant_id);

CREATE TABLE IF NOT EXISTS auth_device_identity (
    tenant_id           TEXT NOT NULL,
    device_id           TEXT NOT NULL,
    status              TEXT NOT NULL,
    trusted             INTEGER NOT NULL DEFAULT 0,
    metadata            TEXT,
    linked_principal_id TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (tenant_id, device_id),
    FOREIGN KEY (linked_principal_id) REFERENCES auth_principal (id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_device_linked_principal
    ON auth_device_identity (linked_principal_id);

CREATE TABLE IF NOT EXISTS auth_policy_document (
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    format      TEXT NOT NULL,
    version     TEXT NOT NULL,
    body        BLOB NOT NULL,
    active      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_auth_policy_active
    ON auth_policy_document (tenant_id, active, updated_at);
