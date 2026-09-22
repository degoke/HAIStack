# haistack-export (`pkg/export`)

FHIR [Bulk Data](https://hl7.org/fhir/uv/bulkdata/) `$export` orchestration for HAIStack.

## What it does

`pkg/export` manages **async bulk export jobs**: kickoff, progress polling, cancellation, NDJSON artifact generation, and export manifest assembly. It scans resources through `store.ResourceStore`, writes files via a pluggable `FileStore`, and persists job state in `JobStore`. Background execution integrates with [`pkg/jobs`](../jobs/README.md) using job type `export.bulk.record`.

HTTP routes are implemented in [`pkg/http`](../http/README.md):

```text
GET    /fhir/$export
GET    /fhir/Group/{id}/$export
GET    /fhir/$export/status/{jobId}
DELETE /fhir/$export/status/{jobId}
GET    /fhir/$export/files/{jobId}/{filename}
```

It does **not** implement REST CRUD, search planning, or long-term object-store SDKs—those live in `core`, `search`, and runtime adapter seams.

## When to use it

- **Research or engineering cohorts** — export Patient/Observation/… as NDJSON with a manifest  
- **Environment migration** — snapshot a tenant before `$import` elsewhere  
- **Compliance archives** — periodic bulk snapshots (with your own retention policy)  

Prefer [`pkg/analytics`](../analytics/README.md) when you need **flat reporting columns**, not resource-shaped NDJSON.

## Usage modes

### 1. Through HTTP (typical)

Enable bulk export in your runtime/HTTP config. Clients call `$export` with `Prefer: respond-async`, poll status, then download files. See [`pkg/client`](../client/README.md) `BulkExport()` helpers.

### 2. Programmatic service

Wire `export.NewService` with `Executor`, job store, and file store (often the tenant `BlobStore`):

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/export"
)

svc, err := export.NewService(export.Config{
    Jobs:     jobStore,   // export.JobStore
    Files:    fileStore,  // export.FileStore (BlobStore adapter)
    Executor: executor,   // export.Executor with ResourceStore scan
    JobQueue: db.JobStore(),
})
if err != nil { /* handle */ }

job, err := svc.Kickoff(ctx, export.KickoffRequest{
    // ResourceTypes, Since, Until, TypeFilter, etc.
})
```

Use `svc.GetJob`, `svc.Cancel`, and `svc.RunJob` (or enqueue via `jobs.Enqueue` with `jobs.TypeExportBulk`).

### 3. CLI backup (related)

`haistack backup` writes NDJSON + manifest from the local store—similar artifact shape, different code path. Use `$export` when you need the standard Bulk Data protocol and async job semantics.

## Where it fits

```text
Client (pkg/client) ──HTTP──► pkg/http ──► pkg/export
                                              │
                                              ├──► pkg/jobs (async)
                                              ├──► store.ResourceStore (scan)
                                              └──► store.BlobStore (artifacts)
```

Runtime wires the service when bulk export is enabled on Postgres/SQLite backends.

## Limits

- Job and file persistence require a configured `JobStore` and `BlobStore` (or stream-capable `FileStoreWithStream`).  
- Very large exports are bounded by disk, scan performance, and job timeout settings in your deployment—not by unbounded in-memory buffers in the executor.  
- Group-level export requires Group membership resolution as implemented in your executor configuration.

## Related docs

- [Bulk Data verification](../../docs/bulk-data-verification.md)  
- [Architecture — transactional vs analytical paths](../../docs/architecture.md#transactional-vs-analytical-paths)
