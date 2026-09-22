// Package export implements FHIR Bulk Data export orchestration for HAIStack.
//
// The export service manages async job lifecycle (kickoff, poll, cancel, manifest),
// scans resources through store.ResourceStore, and writes NDJSON artifacts to a
// FileStore (temp file + PutStream when the store implements FileStoreWithStream).
// Runtime persists job records in store.JobStore (type export.bulk.record)
// and files in store.BlobStore: the configured object-store adapter when present,
// otherwise the chunked Postgres or SQLite blob store. Downloads use OpenFile when
// the file store implements FileStoreWithStream. Background execution integrates with pkg/jobs
// via TypeExportBulk.
//
// HTTP adapters in pkg/http expose:
//   - GET /fhir/$export
//   - GET /fhir/Group/{id}/$export
//   - GET /fhir/$export/status/{jobId}
//   - DELETE /fhir/$export/status/{jobId}
//   - GET /fhir/$export/files/{jobId}/{filename}
package export
