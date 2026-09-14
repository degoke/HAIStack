// Package export implements FHIR Bulk Data export orchestration for HAIStack.
//
// The export service manages async job lifecycle (kickoff, poll, cancel, manifest),
// scans resources through store.ResourceStore, and writes NDJSON artifacts to a
// FileStore. Background execution integrates with pkg/jobs via TypeExportBulk.
//
// HTTP adapters in pkg/http expose:
//   - GET /fhir/$export
//   - GET /fhir/Group/{id}/$export
//   - GET /fhir/$export/status/{jobId}
//   - DELETE /fhir/$export/status/{jobId}
//   - GET /fhir/$export/files/{jobId}/{filename}
package export
