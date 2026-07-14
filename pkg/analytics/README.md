# haistack-analytics (`pkg/analytics`)

Postgres-first analytics and reporting engine built on FHIR ViewDefinitions.

## What it does

`bytefhir-analytics` turns registered views into durable reporting data or export
files. It shares one execution pipeline across two modes:

| Mode | Constant | Target | Use case |
|------|----------|--------|----------|
| **Edge** | `ModeRefresh` | `ReportingTarget` → Postgres reporting tables | Tenant-scoped reporting, dashboards, SQL queries |
| **Cloud** | `ModeExport` | `RowSink` (CSV in v1) | File export, downstream pipelines |

The pipeline is always:

```
registered view → view.Executor → structured rows → target
```

View parsing and FHIRPath stay in `pkg/view`. Analytics only orchestrates
execution and routes rows to the configured destination.

## Quick start

**Register built-in views and refresh a Postgres reporting table:**

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
if err != nil { /* handle */ }

// result.RowCount, result.Metadata.Scanned, result.Metadata.Filtered
```

**Export the same view to CSV:**

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

**Query refreshed reporting data:**

```go
rows, err := tdb.ReportingTableStore().QueryRows(ctx, "patient_summary_view", "1.0.0")
meta, err := tdb.ReportingTableStore().GetMeta(ctx, "patient_summary_view", "1.0.0")
```

## Supported views

First-milestone views (registered via `RegisterBuiltInViews`):

| View name | Resource | Built-in helper |
|-----------|----------|-----------------|
| `patient_summary_view` | Patient | `view.PatientSummaryView()` |
| `appointment_view` | Appointment | `view.AppointmentView()` |
| `observation_view` | Observation | `view.ObservationView()` |

`Runner` rejects views outside `analytics.SupportedViews`. Custom views can be
registered in the view registry for direct `view.Executor` use, but analytics
runs require the allow-listed names in v1.

## Targets and sinks

### ReportingTarget (edge mode)

Writes a full view result into `store.ReportingTableStore`. Each refresh:

1. Deletes existing rows for `(tenant, view_name, view_version)`.
2. Upserts schema metadata (`columns`, `row_count`, `refreshed_at`).
3. Inserts the new row set as ordered JSONB documents.

Postgres implementation: `TenantDB.ReportingTableStore()` in `pkg/postgres`.

This is intentionally separate from `MaterializedViewStore`, which stores opaque
per-key payloads and is not designed for tabular reporting queries.

### CSVSink (cloud mode)

Implements `RowSink`. Encoding rules:

- Headers follow `view.ColumnInfo` declaration order.
- `null` → empty field.
- Scalars → string representation.
- Arrays and maps → JSON in the cell.

### Deferred sinks (interface only)

Constructors exist for wiring tests and future work; all return
`ErrSinkNotImplemented`:

- `NewParquetSink()`
- `NewWarehouseSink()`
- `NewLakehouseSink()`
- `NewManifestExportSink()`

## Background jobs

Runs are synchronous by default but integrate with `pkg/jobs`:

| Job type | Constant | Payload | Handler |
|----------|----------|---------|---------|
| `analytics.refresh` | `analytics.TypeRefresh` | `RefreshPayload` | `RefreshHandler` |
| `export.csv` | `analytics.TypeExport` | `ExportPayload` | `ExportHandler` |

```go
jobRunner := jobs.NewRunner(tdb.JobStore())
jobRunner.Register(analytics.TypeRefresh, analytics.RefreshHandler(runner, target))
jobRunner.Register(analytics.TypeExport, analytics.ExportHandler(runner, csvSink))

_, err := jobs.Enqueue(ctx, tdb.JobStore(), analytics.TypeRefresh, analytics.RefreshPayload{
    ViewName: analytics.ViewPatientSummary,
    Version:  "1.0.0",
}, jobs.EnqueueOptions{})
```

## Where it fits

| Layer | Role |
|-------|------|
| **view** | Parse ViewDefinitions, execute FHIRPath, produce `Result.Rows` |
| **analytics** | Orchestrate view runs into reporting tables or export sinks (this package) |
| **store** | `ReportingTableStore` contract for tabular reporting persistence |
| **postgres** | Tenant-scoped Postgres implementation and migrations |
| **jobs** | Optional async refresh and export scheduling |
| **ai** | Interactive view access via tools; analytics materializes rows for reporting |

## MVP limits

- **Postgres only** for reporting tables; SQLite is not supported.
- **Full refresh only**; no incremental cursors, manifests, or partitioning.
- **Three views** at the Runner allow-list layer.
- **CSV only** as a production cloud sink.
- **No warehouse required**; warehouse/lake/Parquet sinks are deferred.

## Errors

| Error | When |
|-------|------|
| `ErrUnsupportedView` | View name not in `SupportedViews` |
| `ErrUnsupportedDestination` | Mode/destination mismatch (e.g. refresh without `ReportingTarget`) |
| `ErrUnsupportedMode` | Unknown `Mode` value |
| `ErrMissingExecutor` | `NewRunner` without a view executor |
| `ErrSinkNotImplemented` | Deferred sink backends |

View-layer errors (`ErrViewNotFound`, `ErrUnauthorized`, etc.) propagate from
`view.Executor` unchanged.

See [doc.go](./doc.go) for the full API, persistence model, and integration
points.
