-- Blob manifest, chunk, link, sync status, and transfer session tables.
-- Does not modify binary_object (legacy simple store contracts).

CREATE TABLE IF NOT EXISTS blob_manifest (
    blob_id       TEXT PRIMARY KEY,
    sha256        TEXT NOT NULL,
    size          INTEGER NOT NULL,
    content_type  TEXT,
    backend_kind  TEXT NOT NULL,
    storage_ref   TEXT NOT NULL,
    chunk_size    INTEGER,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    finalized_at  TEXT
);

CREATE INDEX IF NOT EXISTS idx_blob_manifest_sha256 ON blob_manifest(sha256);

CREATE TABLE IF NOT EXISTS blob_chunk (
    blob_id      TEXT NOT NULL,
    chunk_index  INTEGER NOT NULL,
    data         BLOB NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (blob_id, chunk_index)
);

CREATE TABLE IF NOT EXISTS blob_binary_link (
    resource_id  TEXT PRIMARY KEY,
    blob_id      TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS blob_document_link (
    document_id    TEXT NOT NULL,
    content_index  INTEGER NOT NULL,
    blob_id        TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    PRIMARY KEY (document_id, content_index)
);

CREATE TABLE IF NOT EXISTS blob_sync_status (
    blob_id     TEXT PRIMARY KEY,
    status      TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS blob_transfer_session (
    session_id          TEXT PRIMARY KEY,
    session_kind        TEXT NOT NULL,
    blob_id             TEXT NOT NULL,
    sha256              TEXT,
    size                INTEGER,
    content_type        TEXT,
    chunk_size          INTEGER,
    transferred_bytes   INTEGER NOT NULL DEFAULT 0,
    transferred_chunks  INTEGER NOT NULL DEFAULT 0,
    expected_chunks     INTEGER,
    total_chunks        INTEGER,
    status              TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
