# haistack-analytics (`pkg/analytics`)

Postgres-first analytics and reporting engine built on FHIR ViewDefinitions.

Import path: `github.com/degoke/haistack/pkg/analytics`.

---

## What it does

`bytefhir-analytics` turns registered views into durable reporting data or export
files. It shares one execution pipeline across two modes:

| Mode | Constant | Target | Use case |
|------|----------|--------|----------|
| **Edge** | `ModeRefresh` | `ReportingTarget` → Postgres reporting tables | Tenant dashboards, `$sqlquery-run` |
| **Cloud** | `ModeExport` | `RowSink` (CSV, NDJSON, Parquet, lakehouse) | Files, pipelines, lake partitions |

The pipeline is always:

```text
registered view → view.Executor → structured rows → destination
```

View parsing and FHIRPath stay in **`pkg/view`**. Analytics only orchestrates
execution and routes rows to the configured destination. `Runner.Run` validates
the view against `SupportedViews`, matches mode to destination, then calls
`view.Executor.Execute` with no row limit so the full matching set is materialized.

Background integration uses **`pkg/jobs`** (`TypeRefresh`, `TypeExport`) with optional
**`WatermarkStore`** for `_since` incremental cursors (`jobs_watermark_test.go`,
`incremental_test.go`).

---

## How it fits in the ecosystem

```text
store.ResourceStore
        │
        ▼
   view.Executor ◄── view.Registry (RegisterBuiltInViews)
        │
        ▼
   analytics.Runner
        │
        ├── ModeRefresh ──► store.ReportingTableStore (pkg/postgres)
        │                         │
        │                         └── view.SQLQueryEngine ($sqlquery-run)
        │
        └── ModeExport ──► RowSink
                              ├── CSVSink
                              ├── ParquetSink / ParquetFileSink
                              ├── LakehouseSink (disk or BlobStore)
                              ├── ManifestExportSink
                              └── WarehouseSink (refresh tables)

pkg/http ──► refresh/export HTTP when wired
pkg/runtime ──► Postgres analytics mode enables reporting + jobs
pkg/ai ──► interactive view access; analytics materializes same rows for reporting
```

| Layer | Role |
|-------|------|
| **view** | Parse ViewDefinitions, execute FHIRPath, produce `Result.Rows` |
| **analytics** | Orchestrate view runs (this package) |
| **store** | `ReportingTableStore` contract |
| **postgres** | Tenant-scoped tables `analytics_reporting_meta` / `_row` |
| **jobs** | Async refresh and export |
| **view.ExportService** | HTTP `$viewdefinition-export` (overlapping sink options) |

---

## When to use it

- **Edge reporting** — refresh Postgres JSONB tables for BI tools and `$sqlquery-run`
- **Scheduled export** — enqueue `analytics.refresh` or `export.csv` jobs
- **Lakehouse partitions** — `NewLakehouseSink` writes `{partition}/{view}-{version}.parquet`
- **CSV handoff** — `\N` null marker and JSON cells for downstream ETL

Do **not** use analytics for ad hoc single-resource field reads (use **`pkg/fhirpath`**).
Do **not** expect SQLite reporting tables in v1 (Postgres only for warehouse refresh).

---

## Usage modes

### 1. Edge refresh (Postgres reporting tables)

From `doc.go` and `integration_postgres_test.go`:

```go
engine, err := fhirpath.NewEngine(fhirpath.Config{})
if err != nil { /* handle */ }

reg := view.NewRegistry()
if err := analytics.RegisterBuiltInViews(reg, engine); err != nil {
    // handle
}

viewExec, err := view.NewExecutor(view.Config{
    Resources: tdb.ResourceStore(),
    Engine:    engine,
    Registry:  reg,
})
if err != nil { /* handle */ }

runner, err := analytics.NewRunner(analytics.Config{Executor: viewExec})
if err != nil { /* handle */ }

result, err := runner.Run(ctx, analytics.RunRequest{
    ViewName: analytics.ViewPatientSummary,
    Mode:     analytics.ModeRefresh,
    Destination: analytics.Destination{
        Reporting: analytics.NewReportingTarget(tdb.ReportingTableStore()),
    },
})
// result.RowCount, result.Metadata.Scanned, result.Metadata.Filtered
```

Query refreshed data:

```go
rows, err := tdb.ReportingTableStore().QueryRows(ctx, "patient_summary_view", "1.0.0")
meta, err := tdb.ReportingTableStore().GetMeta(ctx, "patient_summary_view", "1.0.0")
```

Each refresh deletes existing rows for `(tenant, view_name, view_version)`, upserts
schema metadata, and inserts the new ordered JSONB row set. This is separate from
`MaterializedViewStore` (opaque per-key payloads, not tabular SQL).

### 2. Cloud export (CSV)

From `csv_test.go`:

```go
var buf bytes.Buffer

_, err := runner.Run(ctx, analytics.RunRequest{
    ViewName: analytics.ViewPatientSummary,
    Mode:     analytics.ModeExport,
    Destination: analytics.Destination{
        Sink: analytics.NewCSVSink(&buf),
    },
})
```

CSV rules (`CSVSink`):

- Headers follow `view.ColumnInfo` declaration order.
- `null` or missing field → `\N` (`analytics.CSVNullValue`).
- Empty strings stay empty; leading `\` in scalars is escaped.
- Arrays and maps → JSON in the cell.
- Decimals use fixed-point formatting (no scientific notation).

### 3. Parquet and lakehouse sinks

From `parquet_lakehouse_test.go`:

```go
sink := analytics.NewParquetFileSinkWithConfig(analytics.ParquetFileSinkConfig{
    Writer: &buf,
    Layout: view.ParquetLayoutFHIR,
    Executor: viewExec,
})

lake := analytics.NewLakehouseSink(analytics.LakehouseConfig{
    RootDir: dir,
    ParquetLayout: view.ParquetLayoutFHIR,
    Executor: viewExec,
})
// artifacts under view=<name>/version=<ver>/
```

Blob-backed lakehouse: set `LakehouseConfig.Blob` and `BlobPrefix`.
`NewManifestExportSink` supports manifest-style multi-file exports.
`NewWarehouseSink()` refreshes Postgres reporting tables from export pipelines.

### 4. Incremental refresh / export

Pass `RunRequest.Incremental: true` with a configured `WatermarkStore`:

```go
since, err := watermarks.Since(ctx, analytics.ViewPatientSummary, "1.0.0")
// Runner forwards since to view.ExecuteRequest; advances maxLastUpdated on success
```

Legacy cursor names `analytics.view.*` migrate to `analytics.watermark.*` on first read.
Watermarks advance only after a successful run (`TestExportServiceDoesNotAdvanceWatermarkOnFailure`
in `pkg/view` mirrors the same contract for HTTP export).

### 5. Background jobs

| Job type | Constant | Payload | Handler |
|----------|----------|---------|---------|
| Refresh | `analytics.TypeRefresh` | `RefreshPayload` | `RefreshHandler` |
| Export | `analytics.TypeExport` | `ExportPayload` | `ExportHandler` |

```go
jobRunner := jobs.NewRunner(tdb.JobStore())
jobRunner.Register(analytics.TypeRefresh, analytics.RefreshHandler(runner, target))
jobRunner.Register(analytics.TypeExport, analytics.ExportHandler(runner, csvSink, watermarkStore))

_, err := jobs.Enqueue(ctx, tdb.JobStore(), analytics.TypeRefresh, analytics.RefreshPayload{
    ViewName: analytics.ViewPatientSummary,
    Version:  "1.0.0",
}, jobs.EnqueueOptions{})
```

`ExportHandler` / `ExportHandlerWithConfig` accept optional `*WatermarkStore` (`nil`
disables auto-fill/advance). See [CHANGELOG-parquet-analytics.md](../../docs/CHANGELOG-parquet-analytics.md).

### 6. HTTP and runtime

Postgres analytics mode wires reporting refresh, export operations, and
`$sqlquery-run`. SQLite runtimes still expose view run/export HTTP when storage
and executor are configured; async export persists job metadata when a data
directory and job runner exist.

---

## Supported views

First-milestone views (`views.go`, `contract_test.go`):

| View name | Resource | Built-in helper |
|-----------|----------|-----------------|
| `patient_summary_view` | Patient | `view.PatientSummaryView()` |
| `appointment_view` | Appointment | `view.AppointmentView()` |
| `observation_view` | Observation | `view.ObservationView()` |

Constants: `ViewPatientSummary`, `ViewAppointment`, `ViewObservation`.

`Runner` rejects views outside `analytics.SupportedViews` and rejects custom
definitions registered under a built-in name. Custom views may still be registered
for direct `view.Executor` use outside the Runner allow-list.

Column contracts are asserted in `contract_test.go` (expected column names and types
per packaged view).

---

## Examples from this repo

**CDC / refresh job IDs** (`cdc_test.go`):

```go
jobID := refreshJobID(ViewPatientSummary, "1.0.0")
```

**Incremental watermark round trip** (`incremental_test.go`):

```go
_, err := runner.Run(ctx, analytics.RunRequest{
    ViewName: analytics.ViewPatientSummary,
    Incremental: true,
    // ...
})
since, err := watermarks.Since(ctx, analytics.ViewPatientSummary, "1.0.0")
```

**Postgres integration** (`integration_postgres_test.go`): full refresh, row query,
meta inspection, and export CSV in one tenant DB fixture.

**Parquet lake paths** (`parquet_lakehouse_test.go`): verifies artifact locations contain
`view=patient_summary_view` and `version=1.0.0` partition segments.

---

## Errors

| Error | When |
|-------|------|
| `ErrUnsupportedView` | View name or definition is not a packaged v1 view |
| `ErrUnsupportedDestination` | Mode/destination mismatch (refresh without `ReportingTarget`) |
| `ErrUnsupportedMode` | Unknown `Mode` value |
| `ErrMissingExecutor` | `NewRunner` without a view executor |
| `ErrSinkNotImplemented` | Deferred sink backends |

View-layer errors (`ErrViewNotFound`, `ErrUnauthorized`, etc.) propagate from
`view.Executor` unchanged.

---

## MVP limits

- **Postgres only** for reporting tables and `$sqlquery-run`.
- **Incremental** uses a single `WatermarkStore` per view version; not partitioned output.
- **View export artifacts** on local filesystem (`view-exports/{tenantId}`); rollback on multi-view failure.
- **Three views** at the Runner allow-list layer (direct executor can run other registered views).
- Executor still materializes the complete result set in v1 (streaming sinks reduce upload RAM, not scan memory).

---

## Related docs

- [pkg/view/README.md](../view/README.md) — ViewDefinition execution
- [pkg/postgres/README.md](../postgres/README.md) — reporting table migrations
- [pkg/jobs/README.md](../jobs/README.md) — job runner
- [doc.go](./doc.go) — full API and persistence model
