// Package analytics implements haistack-analytics, a Postgres-first analytics and
// reporting engine for Health AI Stack.
//
// # Scope
//
// bytefhir-analytics is an orchestration layer on top of pkg/view. It does not
// parse ViewDefinitions or evaluate FHIRPath; those responsibilities stay in
// haistack-view. This package owns analytics execution, reporting refresh, and
// export orchestration around a shared pipeline:
//
//  1. Resolve a registered view by name and version.
//  2. Execute it through view.Executor into structured rows.
//  3. Hand rows to a mode-specific target.
//
// v1 supports two modes with one shared core:
//
//   - Edge mode (ModeRefresh): FHIR -> ViewDefinition -> Postgres reporting tables.
//     Full refresh only; each run replaces all rows for the view identity.
//   - Cloud mode (ModeExport): FHIR -> ViewDefinition -> structured rows -> sink
//     adapters. CSV is the only concrete sink in v1.
//
// Warehouse, Parquet, incremental lake export, partitioning, and manifests are
// explicitly deferred. Sink interfaces exist so later backends can be added
// without changing the view execution path.
//
// # Public API
//
// The package centers on Runner and two target abstractions:
//
//   - Runner: synchronous, job-friendly entry point. Accepts a view name,
//     optional version, run mode, and destination configuration.
//   - ReportingTarget: edge-mode writer backed by store.ReportingTableStore.
//     Persists tabular reporting data keyed by tenant, view name, and version.
//   - RowSink: cloud-mode export interface. CSVSink is the v1 implementation.
//
// Additional helpers:
//
//   - RegisterBuiltInViews: register the first-milestone packaged views.
//   - RefreshHandler / ExportHandler: pkg/jobs adapters for background runs.
//   - IsSupportedView / SupportedViews: first-milestone view allow-list.
//
// # Supported views
//
// The first milestone supports exactly three single-resource, flat views:
//
//   - patient_summary_view (Patient)
//   - appointment_view (Appointment)
//   - observation_view (Observation)
//
// Built-in ViewDefinition payloads live in pkg/view (PatientSummaryView,
// AppointmentView, ObservationView). Register them with RegisterBuiltInViews
// or view.Registry.Register before running analytics.
//
// # Edge mode: Postgres reporting tables
//
// Reporting data is stored through store.ReportingTableStore, implemented by
// pkg/postgres (TenantDB.ReportingTableStore). This is separate from
// store.MaterializedViewStore, which persists opaque per-key JSON payloads and
// is not suitable for tabular reporting queries.
//
// Postgres tables (migration 0009_reporting_tables.sql):
//
//   - analytics_reporting_meta: schema and refresh metadata per view version
//   - analytics_reporting_row: JSONB rows ordered by row_num within a refresh
//
// Refresh is tenant-scoped and atomic: delete existing rows for the view
// version, upsert metadata, insert the new row set. SQLite reporting tables
// are not supported in v1.
//
// Typical edge usage:
//
//	engine, _ := fhirpath.NewEngine(fhirpath.Config{})
//	reg := view.NewRegistry()
//	_ = analytics.RegisterBuiltInViews(reg, engine)
//
//	viewExec, _ := view.NewExecutor(view.Config{
//	    Resources: tdb.ResourceStore(),
//	    Engine:    engine,
//	    Registry:  reg,
//	})
//	runner, _ := analytics.NewRunner(analytics.Config{Executor: viewExec})
//
//	_, err := runner.Run(ctx, analytics.RunRequest{
//	    ViewName: analytics.ViewPatientSummary,
//	    Mode:     analytics.ModeRefresh,
//	    Destination: analytics.Destination{
//	        Reporting: analytics.NewReportingTarget(tdb.ReportingTableStore()),
//	    },
//	})
//
// # Cloud mode: CSV export
//
// CSVSink writes view.Result rows to an io.Writer. Column order follows
// view.ColumnInfo declaration order. Null values are empty fields; arrays and
// complex values are JSON-encoded cell strings.
//
//	_, err := runner.Run(ctx, analytics.RunRequest{
//	    ViewName: analytics.ViewPatientSummary,
//	    Mode:     analytics.ModeExport,
//	    Destination: analytics.Destination{
//	        Sink: analytics.NewCSVSink(&buf),
//	    },
//	})
//
// Deferred sink interfaces (stub constructors return ErrSinkNotImplemented):
// ParquetSink, WarehouseSink, LakehouseSink, ManifestExportSink.
//
// # Background jobs
//
// Runs are synchronous in v1 but shaped for pkg/jobs integration:
//
//   - analytics.refresh (jobs.TypeAnalyticsRefresh) with RefreshPayload
//   - export.csv (jobs.TypeExportCSV) with ExportPayload
//
// Register handlers on a jobs.Runner:
//
//	runner.Register(analytics.TypeRefresh, analytics.RefreshHandler(analyticsRunner, target))
//	runner.Register(analytics.TypeExport, analytics.ExportHandler(analyticsRunner, csvSink))
//
// # Execution model
//
// Runner.Run validates the view is in SupportedViews, validates the destination
// matches the mode, then calls view.Executor.Execute with no row limit so the
// full matching resource set is materialized. View parse, authorization, and
// execution errors from pkg/view propagate unchanged.
//
// RunResult reports view identity, mode, row count, and view.ResultMetadata
// (scanned/filtered counts, duration, source resource type).
//
// # Integration points
//
//   - haistack-view: canonical ViewDefinition execution; analytics never duplicates
//     view parsing or FHIRPath logic.
//   - haistack-store: ReportingTableStore contract; Postgres implementation only.
//   - haistack-postgres: tenant-scoped reporting table persistence.
//   - haistack-jobs: optional background refresh and export orchestration.
//   - haistack-ai: complementary; AI tools call view.Executor directly for
//     interactive row access; analytics materializes the same rows for reporting.
//
// # MVP limits
//
//   - Postgres-first reporting; no SQLite reporting tables.
//   - Full refresh only; no incremental cursors or partitioned output.
//   - Three built-in views only at the Runner allow-list layer.
//   - CSV is the only production cloud sink; other sinks are interface-only.
//   - No warehouse dependency required to use analytics.
package analytics
