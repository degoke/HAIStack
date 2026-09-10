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
| FHIR `$viewdefinition-run` operation | Supported | Synchronous run with JSON, CSV, NDJSON, or Parquet output |
| FHIR `$viewdefinition-export` operation | Supported | Async bulk export with watermark-aware `_since` chaining |
| FHIR `$sqlquery-run` operation | Supported | Read-only SQL over reporting tables via embedded SQLite engine |
| Materialized view persistence | Supported | `metadata.materialize` + `Executor.MaterializedViews` / `$materialize` operation |
| FHIRPath `resolve()` | Supported | Typed, absolute URL, and URN references when engine `Resolve` is configured |
| FHIRPath `memberOf()` | Supported | When engine configured with terminology validator |
| Incremental refresh (`_since`) | Supported | Search `_lastUpdated=gt...`, envelope `LastUpdated`, export watermarks, and refresh cursors |
| IG ViewDefinition install | Supported | `packages.Installer` registers views whenever job infrastructure is wired |
| Arbitrary SQL backend | Supported | `$sqlquery-run` over reporting tables; in-process SQLite for ad hoc SELECT |
| Partitioned output / lakehouse sinks | Supported | `LakehouseSink`, `WarehouseSink`, `ManifestExportSink`, and Parquet export |
| SQL-on-FHIR watermark / change detection | Supported | `analytics.WatermarkStore` + CDC cursor advancement |

## Portability story

1. Ship views as JSON ViewDefinition resources in FHIR NPM packages.
2. Add `metadata.searchParams` for index-backed prefilters when search is enabled.
3. Enable materialization with `metadata.materialize=true` or `POST ViewDefinition/$materialize`.
4. Use `forEach` + nested selects or FHIRPath `resolve()` for cross-resource joins.
5. Refresh reporting tables and query them with `$sqlquery-run` or export with `$viewdefinition-export`.

## References

- HAIStack view engine: `pkg/view/README.md`
- Analytics pipeline: `pkg/analytics/README.md`
- HL7 SQL-on-FHIR IG: https://build.fhir.org/ig/HL7/fhir-analytics/
