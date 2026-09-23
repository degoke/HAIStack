# haistack-binary (`pkg/binary`)

Blob and file library for haistack — storage, hashing, manifests, resumable transfer, sync status, and FHIR linkage for Binary and DocumentReference resources.

## What it does

**haistack-binary** owns blob behavior end to end. It stores file payloads separately from FHIR resource metadata, tracks transfer progress, and links blobs to `Binary` and `DocumentReference` resources.

The package enforces a hard separation:

| Path | Carries |
|------|---------|
| **Resource sync** | Blob hash, content type, size, storage pointer only |
| **Blob sync** | Actual payload bytes via chunked/resumable transfer |

No raw payload bytes go into `store.ResourceEvent`, `sync.LocalEvent`, or FHIR resource JSON.

Think of it as: *FHIR resources carry pointers to blobs; blobs live in a dedicated store; upload/download is a separate workflow with resume support.*

### Public concepts

| Type | Role |
|------|------|
| `BlobDescriptor` | Stable blob identity — blob ID, SHA-256, size, content type, backend, storage pointer |
| `StoragePointer` | Opaque backend location, serializable into resource metadata |
| `BlobManifest` | Descriptor plus chunking and creation timestamps |
| `BinaryLink` / `DocumentAttachmentLink` | FHIR resource → blob mappings |
| `BlobSyncStatus` | Transfer lifecycle (`pending`, `uploading`, `downloading`, `complete`, `failed`, …) |
| `UploadSession` / `DownloadSession` | Resumable transfer state |
| `BlobEncryption` / `BlobRetention` | Optional per-blob policy metadata on upload |
| `ChunkSyncPlan` / `SyncChunk` | Chunk-based export/import for separate blob sync pipelines |
| `SignedAccessURL` | Time-limited direct object access (S3-compatible stores) |

### Store interfaces

| Interface | Role |
|-----------|------|
| `BlobStore` | Put, Get, Head, Delete finalized blobs |
| `BlobStoreWithStream` / `BlobStoreWithOpen` | Streaming put/open without full in-memory buffers |
| `ChunkStore` | Append, read, list, delete chunks for resumable transfer |
| `MetadataStore` | Manifests, FHIR links, sync status |
| `TransferStore` | Upload/download session persistence |
| `SignedURLProvider` | Presigned GET/PUT URLs where supported |

Services: `TransferService`, `ChunkSyncService`, `LinkService`, `LifecycleService`.

Backends include **`LocalFileBlobStore`**, SQLite/Postgres chunk stores (via `pkg/sqlite`, `pkg/postgres`), **`S3BlobStore`**, and **`PrefixedFileStore`** over legacy `store.BlobStore`.

Legacy `store.BinaryStore` and `store.BlobStore` (backed by `binary_object`) remain for simple inline storage. Use `pkg/binary` for new blob work.

It does **not**:

- Change the resource-event sync protocol (`pkg/sync` still moves metadata only)
- Parse FHIR or assign version IDs (`pkg/types`, `pkg/core`)
- Automatically run blob transfer inside `pkg/sync.Engine` (application invokes blob sync separately)
- Replace `binary_object` or existing simple store contracts

## How it fits in the ecosystem

```
 pkg/core (FHIR write)
        |
        v
 metadata-only Binary / DocumentReference JSON  +  optional WriteSessionExtension links
        |
        v
 pkg/binary (TransferService, LinkService, FHIR helpers)
        |
   +----+----+----+
   v    v    v    v
 local  sqlite postgres S3
 files  chunks  chunks  objects
        |
        v
 optional ChunkSyncService (bytes) parallel to pkg/sync (metadata)
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **types** | JSON envelope model for FHIR helpers |
| Upstream | **core** | Resource CRUD; metadata-only attachment fields |
| Upstream | **store** | Legacy blob stores; write sessions extended additively |
| Sidecar | **sqlite** / **postgres** | `ChunkBlobStore`, `BlobMetadataStore` adapters |
| Parallel | **sync** | Resource events carry pointers only; blob bytes use `ChunkSyncService` or transfer APIs |

## When to use it

- Storing document attachments, images, PDFs, or other large payloads offloaded from FHIR JSON
- Chunked and resumable upload/download with progress tracking (`DefaultChunkSize` = 1 MiB)
- Hash-based deduplication at finalize time (SHA-256)
- Linking `Binary` and `DocumentReference` resources to stored blobs
- Committing blob link metadata in the same DB transaction as a FHIR write
- S3-compatible object storage with signed URL access
- Optional AES-256-GCM encryption at rest via `KeyResolver`

## Usage modes

### 1. Local file storage (hash-addressed)

```go
import "github.com/degoke/haistack/pkg/binary"

files, err := binary.NewLocalFileBlobStore("/var/haistack/blobs")
desc, err := files.Put(ctx, "blob-1", data, "application/pdf")
got, err := files.GetByHash(ctx, desc.SHA256)
```

Pair with metadata for a unified `BlobStore`:

```go
store := binary.NewLocalFileBlobStoreAdapter(files, metadataStore)
desc, err := store.Put(ctx, "blob-1", data, "application/pdf")
payload, head, err := store.Get(ctx, "blob-1")
```

### 2. SQLite or Postgres chunk backends

```go
blobs := db.ChunkBlobStore()      // sqlite.DB or postgres.TenantDB
meta := db.BlobMetadataStore()
desc, err := blobs.Put(ctx, "blob-1", data, "image/png")
manifest, err := meta.GetManifest(ctx, "blob-1")
```

Backend kinds are recorded on `BlobDescriptor.Backend` (`BackendKindSQLite`, `BackendKindPostgres`, etc.).

### 3. Resumable upload and download (`TransferService`)

```go
xfer, err := binary.NewTransferService(binary.TransferConfig{
    Blobs:     blobs,
    Chunks:    blobs,
    Metadata:  meta,
    Transfers: meta,
    ChunkSize: binary.DefaultChunkSize,
    ResolveKey: keyResolver, // optional encryption
})

upload, err := xfer.StartUpload(ctx, "blob-1", size, "application/pdf", expectedChunks)
// or StartUploadWithOptions for retention/encryption
_, err = xfer.UploadChunk(ctx, upload.ID, 0, chunk0)
manifest, err := xfer.FinalizeUpload(ctx, upload.ID)

download, err := xfer.StartDownload(ctx, "blob-1")
chunk, session, err := xfer.DownloadChunk(ctx, download.ID, 0)
```

### 4. S3-compatible object storage

```go
s3, err := binary.NewS3BlobStore(binary.S3Config{
    Endpoint:        "https://s3.example.com",
    Region:          "us-east-1",
    Bucket:          "clinical-blobs",
    AccessKeyID:     "...",
    SecretAccessKey: "...",
})
desc, err := s3.Put(ctx, blobID, data, contentType)
url, err := s3.SignedGetURL(ctx, blobID, time.Hour)
```

Streaming interfaces (`PutStream`, `OpenBlob`) avoid loading entire objects into memory where the backend supports them.

### 5. FHIR metadata helpers and links

```go
ref := binary.DescriptorToReference(*desc)
binJSON, err := binary.BuildBinaryMetadataJSON("bin-1", "image/png", ref)
docJSON, err := binary.EmbedDocumentAttachment(docJSON, 0, "application/pdf", ref)
refs, err := binary.ExtractBlobReferences(docJSON)
hasBytes := binary.ResourceHasPayloadBytes(docJSON) // should be false

links := binary.NewLinkService(meta)
err = links.LinkBinary(ctx, "bin-res-1", "blob-1")
err = links.LinkDocumentAttachment(ctx, "doc-1", 0, "blob-2")
```

### 6. Transactional metadata with FHIR writes

SQLite and Postgres sessions implement `binary.WriteSessionExtension` additively:

```go
session, err := db.BeginWrite(ctx)
if meta, ok := binary.MetadataFromWriteSession(session); ok {
    _ = meta.PutBinaryLink(ctx, binary.BinaryLink{
        ResourceID: "bin-1",
        BlobID:     "blob-1",
        CreatedAt:  time.Now().UTC(),
    })
}
_ = session.ResourceStore().Create(ctx, envelope)
err = session.Commit(ctx)
```

### 7. Chunk sync export/import (parallel to resource sync)

```go
chunkSync, err := binary.NewChunkSyncService(xfer)
plan, err := chunkSync.ExportPlan(ctx, "blob-1")
chunk, err := chunkSync.ExportChunk(ctx, "blob-1", 0)

session, err := chunkSync.StartImport(ctx, *plan, "", nil)
_, err = chunkSync.ApplyChunk(ctx, session.ID, *chunk)
manifest, err := chunkSync.FinalizeImport(ctx, session.ID)
```

Use when devices or hubs exchange blob bytes outside the FHIR event log.

### 8. Lifecycle and retention (`LifecycleService`)

```go
life, err := binary.NewLifecycleService(blobs, meta)
err = life.DeleteBlob(ctx, blobID, time.Now().UTC()) // ErrRetentionLocked until RetainUntil
```

## Examples

**Stream upload without buffering entire file:**

```go
if streamStore, ok := blobs.(binary.BlobStoreWithStream); ok {
    desc, err := streamStore.PutStream(ctx, blobID, reader, size, contentType)
}
```

**Prefixed files over legacy store (package artifacts):**

```go
prefixed := binary.NewPrefixedFileStore(legacyBlobStore, "packages/", "my.pkg", "application/octet-stream")
```

**Verify attachment carries pointer only before sync:**

```go
if binary.ResourceHasPayloadBytes(docJSON) {
    return fmt.Errorf("inline payload bytes must be stripped before sync")
}
```

## Schema

New blob tables (separate from legacy `binary_object`):

| Table | Purpose |
|-------|---------|
| `blob_manifest` | Stable blob identity and storage pointer |
| `blob_chunk` | Chunked payload bytes |
| `blob_binary_link` | FHIR Binary → blob mapping |
| `blob_document_link` | DocumentReference attachment → blob mapping |
| `blob_sync_status` | Transfer progress per blob |
| `blob_transfer_session` | Resumable upload/download session state |

Migrations: `pkg/sqlite/migrations/0005_blob.sql`, `0006_blob_policy.sql`, `pkg/postgres/migrations/0006_blob.sql`, `0007_blob_policy.sql`.

## Configuration / key types

| Symbol | Role |
|--------|------|
| `DefaultChunkSize` | `1 << 20` (1 MiB) |
| `TransferConfig` | Blobs, chunks, metadata, transfers, chunk size, `KeyResolver` |
| `UploadRequest` | Size, content type, expected chunks, retention, encryption |
| `BackendKind` | `local_file`, `sqlite`, `postgres`, `s3`, … |
| `EncryptionAES256GCM` | Supported via `KeyResolver` |

## Where it fits

| Layer | Role |
|-------|------|
| **store** | Legacy `BinaryStore` / `BlobStore` on `binary_object` |
| **binary** | Rich blob API — manifests, chunks, transfer, links, S3, encryption |
| **sqlite / postgres** | Backend adapters and schema |
| **core** | Resource-focused; integrates via JSON helpers and shared sessions |
| **sync** | Resource metadata events; blob bytes via separate transfer/sync path |
| **http** | May serve downloaded artifacts when wired by runtime (not owned here) |

## Limits

- Hash-based deduplication at finalize; cross-backend garbage collection deferred
- `pkg/sync.Engine` does not invoke blob transfer — orchestrate after metadata sync
- `LocalFileBlobStore` does not persist manifests unless paired with `MetadataStore`
- S3: signed URL and PUT/GET streaming; full multipart upload orchestration is limited
- Encryption requires caller-provided `KeyResolver`; no built-in KMS integration
- Retention enforcement depends on `LifecycleService` invocation — not automatic on all deletes

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — metadata vs payload separation
- [pkg/core/README.md](../core/README.md) — FHIR writes and sessions
- [pkg/sync/README.md](../sync/README.md) — resource-only replication
- [pkg/store/README.md](../store/README.md) — legacy blob store contracts
- [pkg/sqlite/README.md](../sqlite/README.md) / [pkg/postgres/README.md](../postgres/README.md) — chunk/metadata adapters
- [doc.go](./doc.go) — full API and design boundaries
