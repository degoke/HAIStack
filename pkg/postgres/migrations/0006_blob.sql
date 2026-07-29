-- Blob manifest, chunk, link, sync status, and transfer session tables.
-- Does not modify hai_binary_object (legacy simple store contracts).

CREATE TABLE IF NOT EXISTS hai_blob_manifest (
    tenant_id     TEXT NOT NULL,
    blob_id       TEXT NOT NULL,
    sha256        TEXT NOT NULL,
    size          BIGINT NOT NULL,
    content_type  TEXT,
    backend_kind  TEXT NOT NULL,
    storage_ref   TEXT NOT NULL,
    chunk_size    BIGINT,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at  TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, blob_id)
);

CREATE INDEX IF NOT EXISTS idx_blob_manifest_sha256 ON hai_blob_manifest(tenant_id, sha256);

CREATE TABLE IF NOT EXISTS hai_blob_chunk (
    tenant_id     TEXT NOT NULL,
    blob_id       TEXT NOT NULL,
    chunk_index   INTEGER NOT NULL,
    data          BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, blob_id, chunk_index)
);

CREATE TABLE IF NOT EXISTS hai_blob_binary_link (
    tenant_id     TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    blob_id       TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, resource_id)
);

CREATE TABLE IF NOT EXISTS hai_blob_document_link (
    tenant_id       TEXT NOT NULL,
    document_id     TEXT NOT NULL,
    content_index   INTEGER NOT NULL,
    blob_id         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id, content_index)
);

CREATE TABLE IF NOT EXISTS hai_blob_sync_status (
    tenant_id     TEXT NOT NULL,
    blob_id       TEXT NOT NULL,
    status        TEXT NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, blob_id)
);

CREATE TABLE IF NOT EXISTS hai_blob_transfer_session (
    tenant_id           TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    session_kind        TEXT NOT NULL,
    blob_id             TEXT NOT NULL,
    sha256              TEXT,
    size                BIGINT,
    content_type        TEXT,
    chunk_size          BIGINT,
    transferred_bytes   BIGINT NOT NULL DEFAULT 0,
    transferred_chunks  INTEGER NOT NULL DEFAULT 0,
    expected_chunks     INTEGER,
    total_chunks        INTEGER,
    status              TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, session_id)
);
