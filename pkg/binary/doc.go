// Package binary implements haistack-binary, the blob/file library for Health AI Stack.
//
// # Scope
//
// binary owns blob storage behavior, hashing, manifests, resumable transfer, sync
// status, and FHIR linkage for Binary and DocumentReference resources. It enforces
// a hard separation between metadata and payloads:
//
//   - Normal resource sync moves FHIR metadata only.
//   - Blob/file content is transferred by a separate blob sync path.
//   - pkg/postgres and pkg/sqlite provide backend adapters and schema; blob
//     orchestration stays in pkg/binary.
//
// # Public concepts
//
//   - BlobDescriptor: stable blob identity and metadata (blobID, sha256, size,
//     contentType, backend kind, storage pointer).
//   - StoragePointer: opaque backend location metadata, serializable into resource
//     metadata and sync payloads.
//   - BlobManifest: descriptor plus chunking and creation timestamps.
//   - BinaryLink: mapping between a FHIR Binary resource and a blob descriptor.
//   - DocumentAttachmentLink: mapping between a DocumentReference.content[]
//     attachment entry and a blob descriptor.
//   - BlobSyncStatus: pending, uploading, uploaded, downloading, complete, failed.
//   - UploadSession / DownloadSession: resumable transfer state.
//
// # Store interfaces
//
//   - BlobStore: Put, Get, Head, Delete finalized blob payloads.
//   - ChunkStore: chunk append/read/finalize for resumable transfer.
//   - MetadataStore: manifest, link, and sync-status persistence.
//   - TransferStore: upload/download session state.
//
// # Backends
//
// MVP backends:
//
//   - LocalFileBlobStore: hash-addressed files on disk.
//   - SQLiteBlobStore: full blob bytes in SQLite via chunk and manifest tables.
//   - PostgresBlobStore: full blob bytes in Postgres via chunk and manifest tables.
//
// Legacy store.BinaryStore and store.BlobStore remain for simple inline storage;
// pkg/binary is the richer public API for new blob work.
//
// # FHIR integration
//
// Helpers work with the repo's JSON-envelope model, not typed FHIR structs:
//
//   - Build attachment metadata for DocumentReference.content[].attachment.
//   - Build metadata-only Binary resource content.
//   - Extract blob references from normalized resource JSON.
//   - Create/read link rows between resource ids and blob descriptors.
//
// Metadata carried in normal sync/resource paths is limited to blob hash,
// content type, size, and storage pointer. No raw payload bytes go into
// store.ResourceEvent, sync.LocalEvent, or FHIR resource JSON.
package binary
