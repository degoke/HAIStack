// Package bulkimport implements FHIR Bulk Data $import orchestration for HAIStack.
//
// The import service accepts a Parameters kickoff (Prefer: respond-async),
// reads application/fhir+ndjson inputs, and creates or updates resources
// through a ResourceWriter. Background execution integrates with pkg/jobs
// via TypeImportBulk.
//
// Remote input.url values are fetched only when a URLLoader is configured.
// The runtime wires HTTPLoader, which GETs http and https URLs with a 60s
// timeout and a 64MiB size limit. file:// and other non-http(s) schemes are
// rejected; redirects to those schemes are also refused. Tests may omit the
// loader so URL-only kickoffs fail closed.
//
// HTTP adapters in pkg/http expose:
//   - POST /fhir/$import
//   - GET /fhir/$import/status/{jobId}
//   - DELETE /fhir/$import/status/{jobId}
//   - GET /fhir/$import/files/{jobId}/{filename} — download import error NDJSON
package bulkimport
