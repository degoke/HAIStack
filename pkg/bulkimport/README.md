# haistack-bulkimport (`pkg/bulkimport`)

FHIR [Bulk Data](https://hl7.org/fhir/uv/bulkdata/) `$import` orchestration for HAIStack.

## What it does

`pkg/bulkimport` accepts a **Parameters** kickoff (`Prefer: respond-async`), reads **application/fhir+ndjson** inputs, and **creates or updates** resources through a `ResourceWriter` (typically `core.ResourceService`). Job records persist in `store.JobStore` (type `export.import.record`); input and error NDJSON live in `store.BlobStore`. Background work uses [`pkg/jobs`](../jobs/README.md) with `jobs.TypeImportBulk`.

HTTP routes in [`pkg/http`](../http/README.md):

```text
POST   /fhir/$import
GET    /fhir/$import/status/{jobId}
DELETE /fhir/$import/status/{jobId}
GET    /fhir/$import/files/{jobId}/{filename}   # error NDJSON
```

Remote `input.url` values are fetched only when a **`URLLoader`** is configured. Runtime wires `HTTPLoader` for `http`/`https` with size and timeout limits; other schemes are rejected.

## When to use it

- **Restore or migrate** NDJSON produced by `$export` or compatible tools  
- **Batch ingest** from an ETL pipeline that emits FHIR NDJSON  
- **Sandbox seeding** in non-production environments  

Use transactional bundles or single-resource REST when you need fine-grained error handling per request, not million-row batch semantics.

## Usage modes

### 1. HTTP `$import`

POST Parameters with input sources (inline base64 or `url`). Poll status until complete; download error file if present.

### 2. Programmatic service

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/bulkimport"
)

svc, err := bulkimport.NewService(bulkimport.Config{
    Jobs:     jobStore,
    Files:    fileStore,
    Executor: executor, // wraps ResourceWriter + NDJSON parser
    JobQueue: db.JobStore(),
    Loader:   bulkimport.HTTPLoader{}, // optional; omit to reject URL-only kickoffs
})
if err != nil { /* handle */ }

job, err := svc.Kickoff(ctx, bulkimport.KickoffRequest{ /* Parameters body */ })
```

Pair with [`pkg/export`](../export/README.md) for round-trip migration between HAIStack tenants.

## Where it fits

```text
NDJSON ──► pkg/bulkimport ──► core.ResourceService ──► store.WriteSession
                │
                ├──► pkg/jobs
                └──► BlobStore (inputs + OperationOutcome lines)
```

Validation and terminology checks follow whatever you wired into `ResourceService` (same as REST writes).

## Limits

- Without `URLLoader`, kickoffs that only reference remote URLs fail closed.  
- Import does not replace sync push/pull for incremental device replication—use [`pkg/sync`](../sync/README.md) for that model.  
- Error files list per-line failures; partial success semantics match Bulk Data expectations—review job status and error NDJSON before cutover.

## Related docs

- [Bulk Data verification](../../docs/bulk-data-verification.md)  
- [pkg/export/README.md](../export/README.md)
