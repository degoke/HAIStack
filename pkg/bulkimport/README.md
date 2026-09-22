# haistack-bulkimport (`pkg/bulkimport`)

FHIR [Bulk Data](https://hl7.org/fhir/uv/bulkdata/) **`$import`** orchestration for HAIStack.

Accepts a **Parameters** kickoff (`Prefer: respond-async`), reads **application/fhir+ndjson**, and **creates or updates** resources via `ResourceWriter` (usually `core.ResourceService`). Validation matches REST writes.

---

## What it does

| Stage | API | Behavior |
|-------|-----|----------|
| Parse | `ParseParametersKickoff` | Decode `inputFormat`, repeating `input` parts |
| Prepare | `Service.prepareInputs` | Inline NDJSON or fetch `input.url` via `URLLoader` |
| Store inputs | `FileStore.Put` | Path `{jobId}/input-{i}-{Type}.ndjson` |
| Execute | `Executor.Execute` | Line-by-line upsert; error NDJSON for failures |
| Complete | `Manifest` | Per-type counts + error file URLs |

**Job storage:** `jobs.TypeImportBulkRecord`; worker `jobs.TypeImportBulk` + `JobHandler()`.

**HTTP** (`doc.go`, `pkg/http/bulk_import.go`):

```text
POST   /fhir/$import
GET    /fhir/$import/status/{jobId}
DELETE /fhir/$import/status/{jobId}
GET    /fhir/$import/files/{jobId}/{filename}
```

**Remote URLs:** only with `Config.Loader`. Runtime uses `NewHTTPLoader()` — HTTP(S), **60s** timeout, **64MiB** max (`HTTPLoaderTimeout`, `HTTPLoaderMaxBytes`). Rejects `file://` and unsafe redirects.

**Import rules** (`executor.go`):

- Create if id missing; Update if present
- Strips incoming `meta.versionId` / `meta.lastUpdated` before persist (`TestImportStripsMetaVersionIDBeforePersist`)
- Wrong line `resourceType` vs input `type` → error line, continue

Kickoff validation errors use `*core.ServiceError` with `ErrorKindInvalid`.

---

## How it fits in the ecosystem

```text
NDJSON ──► bulkimport.Service ──► Executor ──► core.ResourceService ──► WriteSession
              │                      │
              ├── jobs.TypeImportBulk  └── BlobStore (inputs + errors)
              └── HTTPLoader (optional)
```

- **[`pkg/export`](../export/README.md)** — produces compatible NDJSON for migration.
- **[`pkg/sync`](../sync/README.md)** — incremental replication, not bulk load.
- **`runtime.Services().BulkImportService`** — when job + blob stores wired.

No `BulkImport` helper in `pkg/client` yet — use HTTP or embed `Service`.

---

## When to use it

- Restore/migrate `$export` NDJSON
- ETL batch ingest (one FHIR resource per line)
- Non-prod seed data

Use REST bundles for small/fine-grained control. Use sync for ongoing device replication. Always review error NDJSON before cutover (partial success is normal).

---

## Usage modes

### 1. HTTP `$import`

POST Parameters with `Prefer: respond-async`. Poll `Content-Location` until manifest **200**.

Example body (`TestParseParametersKickoffInlineNDJSON`):

```json
{
  "resourceType": "Parameters",
  "parameter": [
    {"name": "inputFormat", "valueCode": "application/fhir+ndjson"},
    {"name": "input", "part": [
      {"name": "type", "valueCode": "Patient"},
      {"name": "valueString", "valueString": "{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}
    ]}
  ]
}
```

HTTP requires create+update grants on each input resource type (`authorizeImportKickoff`).

### 2. Programmatic service

```go
svc, err := bulkimport.NewService(bulkimport.Config{
    Jobs:     bulkimport.NewDurableJobStore(db.JobStore()),
    Files:    bulkimport.NewBlobFileStore(db.BlobStore()),
    Executor: &bulkimport.Executor{Resources: resourceSvc, Files: files},
    JobQueue: db.JobStore(),
    Loader:   bulkimport.NewHTTPLoader(),
})
job, err := svc.Kickoff(ctx, bulkimport.KickoffRequest{
    InputFormat: bulkimport.InputFormatNDJSON,
    Inputs: []bulkimport.InputFile{{
        Type: "Patient",
        NDJSON: []byte(`{"resourceType":"Patient","id":"p1"}` + "\n" +
            `{"resourceType":"Patient","id":"p2"}`),
    }},
})
manifest := svc.Manifest(job)
runner.Register(jobs.TypeImportBulk, svc.JobHandler())
```

### 3. Parse Parameters only

```go
req, err := bulkimport.ParseParametersKickoff(body)
job, err := svc.Kickoff(ctx, req)
```

Input parts: `type`, `url`, inline via `valueString` / `ndjson` / `resource`.

### 4. URL inputs

```go
_, err := svc.Kickoff(ctx, bulkimport.KickoffRequest{
    Inputs: []bulkimport.InputFile{{
        Type: "Patient",
        URL:  "https://storage.example/Patient.ndjson",
    }},
})
```

Without `Loader` → invalid error (`TestKickoffRejectsURLWithoutLoader`).

### 5. In-memory / sync

Omit `JobQueue` — `TestBulkImportRoundTrip`. Use `RunJob`, `Cancel`, `GetFile` for direct control.

---

## Examples (APIs from this repo)

```go
bulkimport.InputFormatNDJSON // "application/fhir+ndjson"
bulkimport.StatusComplete
```

`TestBulkImportRoundTrip`: two Patient lines → `manifest.Output[0].Count == 2`.

`TestImportUpdatesExistingResource`: same id triggers Update.

`TestRunJobDoesNotCompleteCancelledJob`: mid-write cancel → `StatusCancelled`.

Invalid: empty inputs, `InputFormat: "text/csv"`, URL without loader (`TestKickoffClientErrorsAreInvalid`).

```go
svc.StatusURL(jobID)
svc.FileURL(jobID)
```

---

## Where it fits

```text
POST Parameters ──► pkg/http ──► bulkimport.Kickoff ──► Executor ──► core
```

Error artifacts served from `$import/files/{jobId}/...`.

---

## Limits

- URL fetch requires `URLLoader`; HTTP(S) only; 64MiB default cap per URL.
- Partial success — failed lines do not roll back successful ones.
- Not a sync replacement; not a pkg/client one-liner yet.
- Kickoff failure rolls back staged input blobs when possible.
- Meta version stripped; server assigns new version ids.

---

## Related docs

- [Bulk Data verification](../../docs/bulk-data-verification.md)
- [pkg/export/README.md](../export/README.md)
- [pkg/core/README.md](../core/README.md), [pkg/http/README.md](../http/README.md)
- [pkg/jobs/README.md](../jobs/README.md), [pkg/runtime/README.md](../runtime/README.md)
