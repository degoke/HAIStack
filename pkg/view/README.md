# haistack-view (`pkg/view`)

Execute FHIR `ViewDefinition` resources into structured JSON rows for AI context,
analytics, permissioned data access, and SQL-on-FHIR operations.

Import path: `github.com/degoke/haistack/pkg/view`.

---

## What it does

A FHIR `ViewDefinition` is a declarative tabular projection over FHIR data. This
package turns those definitions into runnable views:

| Stage | API | Behavior |
|-------|-----|----------|
| Parse | `ParseDefinition`, `RegisterViewDefinition` | Validate FHIRPath in columns/filters at register time |
| Register | `Registry.Register` | Named/versioned in-memory catalog |
| Execute | `Executor.Execute` | Scan store, apply filters, expand nested selects |
| Export | `ExportService`, `RunService` | Async NDJSON/CSV/Parquet artifacts with watermarks |
| SQL | `SQLQueryEngine` | Read-only SQL over Postgres reporting tables (analytics mode) |

Core execution flow:

```text
ViewDefinition JSON → ViewSpec → ListIDs/Read → FHIRPath → []map[string]any rows
```

In short: **given a ViewDefinition and a `store.ResourceStore`, produce structured
JSON rows** (optionally paginated, authorized, audited, or materialized).

Built-in packaged views live in `builtins.go` (`PatientSummaryView`, `AppointmentView`,
`ObservationView`). Tests in `view_test.go` and `loader_test.go` exercise parsing,
nested `forEach`, reference joins, and row encoding.

---

## How it fits in the ecosystem

```text
FHIR resources (store.ResourceStore)
        │
        ├── pkg/search ──► candidate IDs (metadata.searchParams)
        │
        ▼
   view.Executor ◄── pkg/fhirpath (columns, where, forEach)
        │
        ├──► pkg/analytics (refresh / export sinks)
        ├──► pkg/ai (LLM context from Result.Rows)
        ├──► pkg/parquetfhir (_parquetLayout=fhir exports)
        ├──► pkg/auth (ViewAuthorizer + permissions[])
        └──► pkg/audit (LogViewAccess)

HTTP: pkg/http wires $viewdefinition-run, $viewdefinition-export,
$materialize, $sqlquery-run when runtime analytics/view services are enabled.
```

| Layer | Role |
|-------|------|
| **search** | Index-backed prefilters (`metadata.searchParams`) |
| **fhirpath** | Field reads, filters, `resolve()`, `memberOf()` |
| **view** | Structured projections (this package) |
| **analytics** | Orchestrate runs into reporting tables or file sinks |
| **ai** | Interactive tools consume the same executor |

Module manifests declare view names in `module.json`; installers register packaged
`ViewDefinition` JSON into a shared registry at runtime startup.

---

## When to use it

- **Reporting columns** — flatten Patient/Observation/Appointment fields for dashboards
- **AI context** — stable JSON rows instead of raw resource blobs
- **Permissioned reads** — declare `permissions` on the view; wire `Authorizer`
- **Lakehouse export** — Parquet-on-FHIR nested layouts via `WriteParquetFHIRExport`
- **Join-like shapes** — nested `forEach` over references resolved through the store

Prefer **`pkg/search`** when you only need resource lists. Prefer **`pkg/fhirpath`**
alone for ad hoc field reads on one envelope. Prefer **`pkg/analytics`** when the
destination is a reporting table or scheduled export job.

---

## Usage modes

### 1. Register and execute (library)

From `doc.go` and `view_test.go` (`TestParseDefinition_ValidView`):

```go
engine, err := fhirpath.NewEngine(fhirpath.Config{})
if err != nil { /* handle */ }

reg := view.NewRegistry()
if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
    // handle
}

exec, err := view.NewExecutor(view.Config{
    Resources: resourceStore,
    Engine:    engine,
    Registry:  reg,
})
if err != nil { /* handle */ }

res, err := exec.Execute(ctx, view.ExecuteRequest{
    ViewName: "patient_summary_view",
    Limit:    10,
})
// res.Rows, res.Total, res.NextOffset, res.Metadata
```

Built-in `patient_summary_view` declares five columns and permission
`read-patient-summary` (see `TestParseDefinition_ValidView`).

### 2. Custom ViewDefinition JSON

Nested `forEach` (one row per phone):

```go
def := []byte(`{
    "resourceType": "ViewDefinition",
    "name": "patient_phones_foreach",
    "version": "1.0.0",
    "resource": "Patient",
    "select": [{
        "column": [{"name": "id", "path": "Patient.id"}],
        "select": [{
            "forEach": "Patient.telecom.where(system = 'phone')",
            "column": [{"name": "phone", "path": "value"}]
        }]
    }]
}`)
spec, err := view.ParseDefinition(def, engine)
_, err = reg.Register(def, engine)
```

Reference join (Appointment → Patient) uses typed references in `forEach` collections;
the executor loads targets via `ResourceStore.Read` (see README join example and
`gap_closure_test.go`).

### 3. Search-driven execution

When `Executor.Config.Search` is wired, views can prefilter with index-backed search:

```json
"metadata": {
  "searchParams": "status=final",
  "searchMode": "auto"
}
```

`searchMode=index` requires search wiring; FHIRPath `where` clauses still apply as
a residual check. `_since` on execute adds `_lastUpdated=gt...` to the search query.

### 4. HTTP SQL-on-FHIR operations

| Operation | Endpoint | Notes |
|-----------|----------|-------|
| `$viewdefinition-run` | `POST /fhir/ViewDefinition/$viewdefinition-run` or `POST /fhir/$viewdefinition-run` | Sync JSON/CSV/NDJSON/Parquet |
| `$viewdefinition-export` | `POST /fhir/ViewDefinition/$viewdefinition-export` | Async export; `_since` watermarks |
| `$materialize` | `POST /fhir/ViewDefinition/$materialize` | Async materialized view refresh |
| `$sqlquery-run` | `POST /fhir/Library/$sqlquery-run` | Read-only SQL over reporting tables |

Export artifacts download at
`$viewdefinition-export/files/{jobId}/{filename}`. Async jobs persist under
`{dataDir}/jobs/*` when `runtime.WithDataDir` is set.

Query/body parameters: `_subject`, `_actor`, `_format`, `_parquetLayout`,
`_parquetTimestampEncoding` (see Limits below). `ExportService.Kickoff` defaults
to NDJSON when no format is specified.

### 5. Materialized views

```go
exec, err := view.NewExecutor(view.Config{
    Resources:         resourceStore,
    Engine:            engine,
    Registry:          reg,
    MaterializedViews: materializedViewStore,
})
// View metadata: {"materialize": "true", "materializeKey": "id"}
res, err := exec.Execute(ctx, view.ExecuteRequest{
    ViewName: "patient_summary_materialized",
})
```

Requires `ViewMaterializeService` for HTTP `$materialize` (Postgres analytics wiring).

### 6. Authorization and audit

Optional `Config.Authorizer` enforces declared `permissions` before reads.
Optional `Config.AuditLogger` records success, denial, and resolution errors.
Both seams are optional for embedded/library use.

### 7. Parquet and large exports

`WriteParquetFHIRExport` streams through temp NDJSON spill and row groups (default
1000 rows). Prefer it over `CollectMatchingResources` for large datasets — the
latter materializes every match in memory (`export_service_test.go`,
`parquet_fhir_export.go`).

`_parquetLayout=flat` streams flat columns; `_parquetLayout=fhir` emits Parquet-on-FHIR
nested layouts via `pkg/parquetfhir`. Timestamp annotations default to INT64
TIMESTAMP(MILLIS); `_parquetTimestampEncoding=int96` selects spec INT96.

---

## Supported ViewDefinition subset

- One source resource type per view (`resource`).
- Root `select` array (multiple entries cross-join).
- Nested `select`, `forEach`, `forEachOrNull`, and `unionAll`.
- Column paths prefixed with a resource type (for example `Patient.id`) evaluate
  against the root resource; relative paths evaluate against the current `forEach` item.
- Typed relative references in `forEach` collections resolve through the resource store.
- Optional root filters (`where`) as FHIRPath predicates.
- Materialization via `metadata.materialize` / `metadata.materializeKey`.
- Declared permissions as top-level `permissions` (v1 extension).

---

## Row encoding

From `encode.go` and `encode_test.go`:

- Empty FHIRPath result → `null` (or `[]` when `collection: true`)
- Singleton scalar → JSON scalar (or one-element array when `collection: true`)
- Multi-item result → JSON array; scalar columns without `collection: true` → `ErrRowEncoding`
- Google FHIR `Date`, `DateTime`, `Time`, `Instant` → FHIR string literals
- FHIR choice wrappers unwrap to their set branch
- `system.Quantity` → `{value, unit, system, code}`
- Unsupported complex objects → `ErrRowEncoding`

Configure FHIRPath with `Resolve` and `Terminology` for `resolve()` and `memberOf()`.

---

## Execution metadata

`ResultMetadata` fields differ by export mode:

| Field | Flat `Execute` | FHIR parquet export |
|-------|----------------|---------------------|
| `scanned` | Candidate IDs considered | Same |
| `filtered` | Expanded **view row** count | Matching **source resource** count |
| `maxLastUpdated` | Latest among returned rows | Latest among exported resources |

Incremental watermarks prefer `maxLastUpdated` (data clock). Stored watermarks are
inclusive; search prefilters use `_lastUpdated=gt{watermark}` while envelope checks
use strict `LastUpdated.Before(since)`.

---

## Examples from this repo

**Registry helper** (`loader_test.go`):

```go
spec, err := view.RegisterViewDefinition(reg, view.PatientSummaryView(), engine)
```

**Export watermark behavior** (`export_service_test.go`):

- `TestExportServiceAdvancesWatermarkAfterAllViewsSucceed`
- `TestExportServiceDoesNotAdvanceWatermarkOnFailure`
- `TestExportServiceRollsBackPartialFilesOnFailure`

**SQL over reporting tables** (`gap_closure_test.go` `TestSQLQueryEngine_SelectReportingRows`):

```go
engine, _ := view.NewSQLQueryEngine(view.SQLQueryConfig{
    Reporting: reportingTableStore,
})
rows, err := engine.Query(ctx, "SELECT id, given FROM patient_summary_view")
```

**Contained reference resolution** (`gap_closure_test.go` `TestResolveContainedReference`).

---

## Limits

- Reference resolution: typed, absolute URL, URN, contained `#`, and FHIRPath `resolve()`.
- `$sqlquery-run` requires Postgres analytics wiring and refreshed reporting tables.
- SQLite/edge runtimes support view run/export HTTP when executor is wired; async
  export/materialize needs a job runner and data directory.
- Search-driven execution requires search wiring; `searchMode=index` fails without index.
- `ExecuteRequest.Parameters` is passed to auth and audit only (no FHIRPath substitution yet).
- Parameters parsing accepts `valueString` wrappers only (typed FHIR parameters TODO).
- Postgres `store.BlobStore` BYTEA still materializes full blobs; prefer object-store
  adapters or lakehouse filesystem for multi-GB parquet uploads.

---

## Related docs

- [pkg/analytics/README.md](../analytics/README.md) — reporting refresh and export orchestration
- [pkg/fhirpath/README.md](../fhirpath/README.md) — expression engine
- [pkg/parquetfhir/README.md](../parquetfhir/README.md) — nested parquet layout
- [pkg/search/README.md](../search/README.md) — index-backed prefilters
- [doc.go](./doc.go) — full API and integration points
