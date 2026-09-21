// Package bulkimport implements FHIR Bulk Data $import orchestration for HAIStack.
//
// The import service accepts a Parameters kickoff (Prefer: respond-async),
// reads application/fhir+ndjson inputs, and creates or updates resources
// through a ResourceWriter. Background execution integrates with pkg/jobs
// via TypeImportBulk.
//
// HTTP adapters in pkg/http expose:
//   - POST /fhir/$import
//   - GET /fhir/$import/status/{jobId}
//   - DELETE /fhir/$import/status/{jobId}
package bulkimport
