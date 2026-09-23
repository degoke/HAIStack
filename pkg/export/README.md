# haistack-export (`pkg/export`)

FHIR [Bulk Data](https://hl7.org/fhir/uv/bulkdata/) **`$export`** orchestration for HAIStack.

This package implements async bulk export: job kickoff, polling, cancellation, NDJSON artifacts, and manifest assembly. [`pkg/http`](../http/README.md) calls into it for `$export` routes; this package is not an HTTP server.

---

## What it does

`pkg/export` owns the **bulk export job lifecycle**:

| Stage | Code | Behavior |
|-------|------|----------|
| Kickoff | `Service.Kickoff` | Create job, enqueue `jobs.TypeExportBulk` or run inline |
| Execute | `Executor.Execute` | List IDs per type, read envelopes, write NDJSON lines |
| Poll | `Service.GetJob`, HTTP status | Progress string, manifest when `StatusComplete` |
| Download | `OpenFile`, `GetFile` | Stream artifacts from `FileStore` |
| Cancel | `Service.Cancel` | Sets `CancelRequested`; executor checks between rows |

**Types** (`types.go`): `KickoffRequest`, `Job`, `Manifest`, `OutputFile`, `JobPayload`. Status constants: `StatusInProgress`, `StatusComplete`, `StatusError`, `StatusCancelled`.

**Persistence:**

- Jobs: `NewDurableJobStore(db.JobStore())` uses `jobs.TypeExportBulkRecord` (status rows are not worker jobs).
- Files: `NewBlobFileStore(blobs)` stores under prefix `bulk-export/` as `application/fhir+ndjson`.
- Worker payload: `{ "jobId": "..." }`; handler via `svc.JobHandler()`.

**HTTP routes** (`doc.go`, `pkg/http/bulk.go`):

```text
GET    /fhir/$export
GET    /fhir/Group/{id}/$export
GET    /fhir/Patient/{id}/$export
GET    /fhir/Patient/$export
GET    /fhir/$export/status/{jobId}
DELETE /fhir/$export/status/{jobId}
GET    /fhir/$export/files/{jobId}/{filename}
```

Kickoff requires `Prefer: respond-async` (HTTP). Response **202** with `Content-Location` = `Service.StatusURL(jobID)`.

The **Executor** streams NDJSON through temp files and prefers `FileStoreWithStream.PutStream` (`TestExecutorStreamsPutWithoutBufferedPut`). Filters: `_since` vs `LastUpdated`, Group/Patient scoping via `ParseGroupPatientIDs` and reference fields on resources.

Does **not** implement REST CRUD, search, SMART scopes, or cloud SDKs.

---

## How it fits in the ecosystem

```text
Client ──HTTP──► pkg/http ──► export.Service
                                  ├── jobs.TypeExportBulk
                                  ├── ResourceStore (Executor)
                                  └── BlobStore (artifacts)

pkg/client.BulkExport() ── same HTTP contract
```

- **[`pkg/core`](../core/README.md)** — validation/versioning on writes; export **reads** only.
- **[`pkg/bulkimport`](../bulkimport/README.md)** — symmetric `$import` for migration.
- **[`pkg/analytics`](../analytics/README.md)** — flat columns, not Bulk Data NDJSON.
- **`runtime.Services().BulkExportService`** — wired when job + blob stores exist (`wire.go`).

CLI `haistack backup` produces similar NDJSON without Bulk Data job semantics.

---

## When to use it

- **Cohort export** — Patient/Observation/… as standard NDJSON + manifest
- **Tenant migration** — snapshot before `$import` elsewhere
- **Compliance archives** — periodic snapshots (retention is yours)
- **Interop tests** — [`pkg/client`](../client/README.md) `BulkExport()`

Prefer REST search for UI. Prefer analytics for SQL reporting. Prefer CLI backup when you do not need async Bulk Data URLs.

---

## Usage modes

### 1. HTTP (typical)

Enable bulk export in runtime. Client flow: kickoff → poll status URL → GET each `output[].url`. Auth sets `requiresAccessToken` on manifest when configured.

### 2. Programmatic `export.Service`

```go
exportJobs := export.NewDurableJobStore(db.JobStore())
exportFiles := export.NewBlobFileStore(db.BlobStore())
executor := &export.Executor{Resources: db.ResourceStore(), Files: exportFiles, Types: snap}

svc, err := export.NewService(export.Config{
    Jobs: exportJobs, Files: exportFiles, Executor: executor,
    JobQueue: db.JobStore(), PublicURL: "https://host/fhir",
})
job, err := svc.Kickoff(ctx, export.KickoffRequest{
    ResourceTypes: []string{"Patient"},
    RequestURL:    "GET /fhir/$export?_type=Patient",
})
runner.Register(jobs.TypeExportBulk, svc.JobHandler())
```

Omit `JobQueue` for synchronous `RunJob` inside `Kickoff` (unit tests).

### 3. In-memory tests

`NewInMemoryJobStore`, `NewInMemoryFileStore` — see `service_test.go` `TestBulkExportRoundTrip`.

### 4. `pkg/client` helper

```go
job, _ := c.BulkExport().Kickoff(ctx, client.ExportKickoffRequest{ResourceTypes: []string{"Patient"}})
completed, _ := c.BulkExport().Wait(ctx, job.StatusURL, 5*time.Second)
manifest, _ := c.BulkExport().GetManifest(ctx, completed.StatusURL)
_ = c.BulkExport().Cancel(ctx, job.StatusURL)
```

See `runtime_integration_test.go` for runtime + client round trip.

### 5. Maintenance APIs

`RunJob(ctx, id)`, `Cancel(ctx, id)`, `DeleteJobFiles(ctx, id)`, `Manifest(job)`, `StatusURL`, `FileURL`.

---

## Examples (APIs from this repo)

From `TestBulkExportRoundTrip`:

```go
job, err := svc.Kickoff(ctx, export.KickoffRequest{
    ResourceTypes: []string{"Patient"},
    RequestURL:    "GET /fhir/$export?_type=Patient",
})
manifest := svc.Manifest(job)
data, _, err := svc.GetFile(ctx, job.ID, "Patient.ndjson")
rc, _, err := svc.OpenFile(ctx, job.ID, "Patient.ndjson")
```

`NewService` errors: `"export: JobStore is required"`, same for FileStore and Executor.

Default types when `_type` omitted and no `TypeCatalog`: `Patient`, `Observation`, `Appointment` (`executor.go`).

Manifest JSON keys validated in `TestManifestSchema`: `transactionTime`, `request`, `requiresAccessToken`, `output`.

---

## Where it fits

```text
Client (pkg/client) ──HTTP──► pkg/http ──► pkg/export
                                              │
                                              ├──► pkg/jobs
                                              ├──► store.ResourceStore
                                              └──► store.BlobStore
```

Completed jobs survive restart when using durable stores (`TestRuntimeBulkExportPersistsAcrossRestart`).

---

## Limits

- Requires `JobStore` + `BlobStore` (or stream-capable file store); else HTTP not implemented.
- Scale limited by ID listing (pages of 100), disk, and worker timeouts — not unbounded RAM.
- Group export needs readable `Group`; patient filter uses common reference fields only.
- `_typeFilter` stored on request; full Bulk Data `_until` may need custom extensions.
- Not "search result export" unless data is pre-scoped (Group, Patient, `_since`).

---

## Related docs

- [docs/architecture.md](../../docs/architecture.md#transactional-vs-analytical-paths)
- [Architecture — transactional vs analytical paths](../../docs/architecture.md#transactional-vs-analytical-paths)
- [pkg/http/README.md](../http/README.md), [pkg/client/README.md](../client/README.md)
- [pkg/jobs/README.md](../jobs/README.md), [pkg/bulkimport/README.md](../bulkimport/README.md)
- [pkg/runtime/README.md](../runtime/README.md)
