# haistack-binary (`pkg/binary`)

Blob and file library for health-ai-stack — storage, hashing, manifests, resumable transfer, sync status, and FHIR linkage for Binary and DocumentReference resources.

## What it does

**haistack-binary** owns blob behavior end to end. It stores file payloads separately from FHIR resource metadata, tracks transfer progress, and links blobs to `Binary` and `DocumentReference` resources.

The package enforces a hard separation:

| Path | Carries |
|------|---------|
| **Resource sync** | Blob hash, content type, size, storage pointer only |
| **Blob sync** | Actual payload bytes via chunked/resumable transfer |

No raw payload bytes go into `store.ResourceEvent`, `sync.LocalEvent`, or FHIR resource JSON.

Think of it as: *FHIR resources carry pointers to blobs; blobs live in a dedicated store; upload/download is a separate workflow with resume support.*

```
pkg/core (FHIR write)  →  metadata-only JSON + blob link rows
                              ↓
pkg/binary             →  manifest, chunks, sync status, transfer sessions
                              ↓
local file / sqlite / postgres backends
```

### Public concepts

| Type | Role |
|------|------|
| `BlobDescriptor` | Stable blob identity — blob ID, SHA-256, size, content type, backend, storage pointer |
| `StoragePointer` | Opaque backend location, serializable into resource metadata |
| `BlobManifest` | Descriptor plus chunking and creation timestamps |
| `BinaryLink` | Maps a FHIR `Binary` resource ID to a blob |
| `DocumentAttachmentLink` | Maps `DocumentReference.content[].attachment` to a blob |
| `BlobSyncStatus` | `pending` → `uploading`/`downloading` → `complete` / `failed` |
| `UploadSession` / `DownloadSession` | Resumable transfer state |

### Store interfaces

| Interface | Role |
|-----------|------|
| `BlobStore` | Put, Get, Head, Delete finalized blobs |
| `ChunkStore` | Append, read, list, delete chunks for resumable transfer |
| `MetadataStore` | Manifests, FHIR links, sync status |
| `TransferStore` | Upload/download session persistence |

MVP backends:

- **`LocalFileBlobStore`** — hash-addressed files on disk
- **`sqlite.ChunkBlobStore`** — full blob bytes in SQLite chunk tables
- **`postgres.BlobChunkStore`** — full blob bytes in Postgres chunk tables (tenant-scoped)

Legacy `store.BinaryStore` and `store.BlobStore` (backed by `binary_object`) remain for simple inline storage. Use `pkg/binary` for new blob work.

It does **not**:

- Change the resource-event sync protocol (`pkg/sync` still moves metadata only)
- Parse FHIR or assign version IDs (`pkg/types`, `pkg/core`)
- Provide S3, signed URLs, per-blob encryption, or retention policies (deferred)
- Replace `binary_object` or existing simple store contracts

## When to use it

- Storing document attachments, images, PDFs, or other large payloads offloaded from FHIR JSON
- Chunked and resumable upload/download with progress tracking
- Hash-based deduplication at the manifest layer
- Linking `Binary` and `DocumentReference` resources to stored blobs
- Committing blob link metadata in the same DB transaction as a FHIR write

## Usage

### Local file storage

```go
import (
    "context"

    "github.com/degoke/health-ai-stack/pkg/binary"
)

files, err := binary.NewLocalFileBlobStore("/var/haistack/blobs")
if err != nil {
    // handle error
}

desc, err := files.Put(ctx, "blob-1", data, "application/pdf")
// desc.SHA256, desc.Size, desc.Pointer.Ref

got, err := files.GetByHash(ctx, desc.SHA256)
```

Combine with a metadata store for a full `BlobStore`:

```go
store := binary.NewLocalFileBlobStoreAdapter(files, metadataStore)
desc, err := store.Put(ctx, "blob-1", data, "application/pdf")
payload, head, err := store.Get(ctx, "blob-1")
```

### SQLite backend

```go
import "github.com/degoke/health-ai-stack/pkg/sqlite"

db, _ := sqlite.Open("/path/to/haistack.db")
_ = db.Migrate(ctx)

blobs := db.ChunkBlobStore()
meta := db.BlobMetadataStore()

desc, err := blobs.Put(ctx, "blob-1", data, "image/png")
manifest, err := meta.GetManifest(ctx, "blob-1")
```

### Postgres backend

```go
import "github.com/degoke/health-ai-stack/pkg/postgres"

tdb := pdb.Tenant("tenant-a")
blobs := tdb.ChunkBlobStore()
meta := tdb.BlobMetadataStore()
```

### Resumable upload and download

```go
xfer, err := binary.NewTransferService(binary.TransferConfig{
    Blobs:     blobs,
    Chunks:    blobs,       // ChunkBlobStore implements both
    Metadata:  meta,
    Transfers: meta,        // BlobMetadataStore implements TransferStore
    ChunkSize: binary.DefaultChunkSize,
})

upload, err := xfer.StartUpload(ctx, "blob-1", size, "application/pdf", expectedChunks)
_, err = xfer.UploadChunk(ctx, upload.ID, 0, chunk0)
_, err = xfer.UploadChunk(ctx, upload.ID, 1, chunk1)
manifest, err := xfer.FinalizeUpload(ctx, upload.ID) // deduplicates by SHA-256

download, err := xfer.StartDownload(ctx, "blob-1")
chunk, session, err := xfer.DownloadChunk(ctx, download.ID, 0)
```

### FHIR metadata helpers

Helpers work with the repo's JSON-envelope model, not typed FHIR structs:

```go
ref := binary.DescriptorToReference(*desc)

// Metadata-only Binary resource (no data element)
binJSON, err := binary.BuildBinaryMetadataJSON("bin-1", "image/png", ref)

// Embed attachment metadata into DocumentReference JSON
docJSON, err := binary.EmbedDocumentAttachment(docJSON, 0, "application/pdf", ref)

// Extract references; verify no inline payload bytes
refs, err := binary.ExtractBlobReferences(docJSON)
hasBytes := binary.ResourceHasPayloadBytes(docJSON) // should be false
```

### Resource links

```go
links := binary.NewLinkService(meta)

err = links.LinkBinary(ctx, "bin-res-1", "blob-1")
err = links.LinkDocumentAttachment(ctx, "doc-1", 0, "blob-2")
```

### Transactional metadata with FHIR writes

SQLite and Postgres sessions implement `binary.WriteSessionExtension` additively — `store.WriteSession` is unchanged:

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

Migrations: `pkg/sqlite/migrations/0005_blob.sql`, `pkg/postgres/migrations/0006_blob.sql`.

## Mental model

```
pkg/types   → canonical FHIR JSON (ResourceEnvelope)
pkg/core    → resource CRUD; metadata-only sync path
pkg/binary  → blob payloads, manifests, transfer, FHIR linkage
pkg/sqlite  → ChunkBlobStore + BlobMetadataStore adapters
pkg/postgres → BlobChunkStore + BlobMetadataStore adapters (tenant-scoped)
```

**One line:** `pkg/binary` is the **file cabinet for clinical attachments** — payloads stay out of FHIR JSON and resource events; metadata travels through normal sync; bytes move through a dedicated blob path.

## Where it fits

| Layer | Role |
|-------|------|
| **store** | Legacy `BinaryStore` / `BlobStore` on `binary_object` |
| **binary** | Rich blob API — manifests, chunks, transfer, links |
| **sqlite / postgres** | Backend adapters and schema only |
| **core** | Resource-focused; integrates via JSON helpers and optional shared sessions |
| **sync** | Resource metadata events unchanged; blob sync invoked separately |

## MVP limits

- Hash-based deduplication at the manifest layer; cross-backend garbage collection deferred
- `S3BlobStore`, signed URLs, per-blob encryption, and retention policies are out of scope
- Blob sync is not wired into `pkg/sync` engine yet — application code invokes blob transfer after resource metadata sync
- `LocalFileBlobStore` does not persist manifests unless paired with a `MetadataStore`

See [doc.go](./doc.go) for the full API and design boundaries.
