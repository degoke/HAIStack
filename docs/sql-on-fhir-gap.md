# SQL-on-FHIR gap analysis (HAIStack ViewDefinition v2)

This document maps the HAIStack ViewDefinition implementation in `pkg/view` to the [HL7 SQL-on-FHIR](https://build.fhir.org/ig/HL7/fhir-analytics/) analytics specification.

## Supported

| SQL-on-FHIR concept | HAIStack status | Notes |
|---------------------|-----------------|-------|
| Single resource `select` | Supported | One root resource type per view |
| Flat and nested column projections | Supported | FHIRPath expressions with `forEach`, nested `select`, `unionAll` |
| `where` filters | Supported | FHIRPath boolean filters (residual when search prefilter is used) |
| `forEach` / `forEachOrNull` | Supported | Row expansion with `%i` focus evaluation for relative paths |
| Nested `select` / `unionAll` | Supported | Cross join and concatenation semantics |
| Reference joins | Supported | Typed, absolute URL, URN, and contained `#` references via resource store |
| View name + version | Supported | Registry key `name\|version` |
| Packaged built-in views | Supported | Built-ins include `metadata.searchParams` where indexable |
| Search-driven candidate resolution | Supported | `metadata.searchParams` + optional `metadata.searchMode` (`auto`, `index`, `scan`) |
| FHIR `$materialize` operation | Supported | `POST /fhir/ViewDefinition/$materialize` with async polling at `$materialize/status/{jobId}` |
| FHIR `$viewdefinition-run` operation | Supported | Sync run on type or system route; JSON, CSV, NDJSON, or Apache Parquet binary output |
| FHIR `$viewdefinition-export` operation | Supported | Async bulk export with watermark-aware `_since`; artifacts at `$viewdefinition-export/files/{jobId}/{filename}` |
| FHIR `$sqlquery-run` operation | Supported | Read-only SQL over reporting tables via embedded SQLite engine (Library or system route) |
| Materialized view persistence | Supported | `metadata.materialize` + `Executor.MaterializedViews` / `$materialize` operation |
| FHIRPath `resolve()` | Supported | Typed, absolute URL, URN, and contained `#` references when engine `Resolve` is configured |
| FHIRPath `memberOf()` | Supported | When engine configured with terminology validator |
| Incremental refresh (`_since`) | Supported | Search `_lastUpdated=gt...`, envelope `LastUpdated`, export watermarks advanced only after successful refresh/export |
| IG ViewDefinition install | Supported | `packages.Installer` registers views; async via job queue when present, otherwise `DirectPackageInstallService` runs synchronously |
| Arbitrary SQL backend | Supported | `$sqlquery-run` over reporting tables; in-process SQLite for ad hoc SELECT; requires a reporting store (Postgres analytics mode) |
| Partitioned output / lakehouse sinks | Supported | `LakehouseSink`, `WarehouseSink`, `ManifestExportSink`; flat view Apache Parquet binary (`application/vnd.apache.parquet`); lakehouse writes `{partition}/{view}-{version}.parquet` to filesystem or blob store |
| Parquet-on-FHIR nested resource export | Supported | `_parquetLayout=fhir` on `$viewdefinition-run` and `$viewdefinition-export`; schema derived from base StructureDefinition via `pkg/parquetfhir` |
| SQL-on-FHIR watermark / change detection | Supported | `analytics.WatermarkStore`; legacy `analytics.view.*` cursors migrate to watermarks on first read; CDC enqueues refresh jobs; watermarks advance in the refresh/export handler after success |

## Runtime availability

| Capability | SQLite / edge | Postgres + analytics |
|------------|---------------|----------------------|
| `$viewdefinition-run` | Yes (always wired with storage; async export/materialize when job store exists) | Yes |
| `$viewdefinition-export` + file download | Yes (filesystem-backed export artifacts and job metadata when storage is configured) | Yes |
| `$sqlquery-run` | No reporting tables | Yes |
| Reporting refresh + CDC watermarks | No | Yes (`WithAnalytics()`) |

## Portability story

1. Ship views as JSON ViewDefinition resources in FHIR NPM packages.
2. Add `metadata.searchParams` for index-backed prefilters when search is enabled.
3. Enable materialization with `metadata.materialize=true` or `POST ViewDefinition/$materialize`.
4. Use `forEach` + nested selects or FHIRPath `resolve()` for cross-resource joins.
5. Refresh reporting tables and query them with `$sqlquery-run` or export with `$viewdefinition-export`.

## Runtime data directory

Configure durable view export artifacts and async job metadata with `runtime.Builder.WithDataDir()` or `WithViewExportDir()`. SQLite runtimes default to `{sqlite-dir}/view-exports`; Postgres runtimes default to `view-exports/{tenantId}`. Paths are resolved to absolute filesystem locations at wire time. Job records are stored under `{dataDir}/jobs/view-export` and `{dataDir}/jobs/materialize`.

Parquet export supports two layouts via `_parquetLayout`:
- `flat` (default): ViewDefinition column schemas streamed through `WriteParquetExport`.
- `fhir`: Parquet-on-FHIR nested resource layout derived from base StructureDefinitions (`pkg/parquetfhir`); exports full matching source resources, not flat view rows.

## References

- HAIStack view engine: `pkg/view/README.md`
- Analytics pipeline: `pkg/analytics/README.md`
- HL7 SQL-on-FHIR IG: https://build.fhir.org/ig/HL7/fhir-analytics/
