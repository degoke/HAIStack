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
| Reference joins | Supported | Typed `ResourceType/id` via resource store; FHIRPath `resolve()` when engine configured |
| View name + version | Supported | Registry key `name\|version` |
| Packaged built-in views | Supported | Built-ins include `metadata.searchParams` where indexable |
| Search-driven candidate resolution | Supported | `metadata.searchParams` + optional `metadata.searchMode` (`auto`, `index`, `scan`) |
| FHIR `$materialize` operation | Supported | `POST /fhir/ViewDefinition/$materialize` with async polling at `$materialize/status/{jobId}` |
| Materialized view persistence | Supported | `metadata.materialize` + `Executor.MaterializedViews` / `$materialize` operation |
| FHIRPath `memberOf()` | Supported | When engine configured with terminology validator |
| Incremental refresh (`_since`) | Partial | Search `_lastUpdated=gt...` or envelope `LastUpdated` on scan fallback |
| IG ViewDefinition install | Partial | `packages.Installer` registers views when analytics is wired |

## Not supported (deferred)

| SQL-on-FHIR concept | HAIStack status | Priority |
|---------------------|-----------------|----------|
| Arbitrary SQL backend | Out of scope | By design — in-process execution |
| Absolute URL / URN reference resolution in `resolve()` | Partial | Typed relative refs only in v2 |
| FHIRPath `resolve()` without configured resolver | Rejected | Configure engine `Resolve` or use view-engine reference joins |
| Partitioned output / lakehouse sinks | Stub interfaces only | P3 — `ParquetWarehouseAdapter` seam |
| Full SQL-on-FHIR watermark / change detection | Partial | Analytics CDC uses outbox cursors |

## Portability story

1. Ship views as JSON ViewDefinition resources in FHIR NPM packages.
2. Add `metadata.searchParams` for index-backed prefilters when search is enabled.
3. Enable materialization with `metadata.materialize=true` or `POST ViewDefinition/$materialize`.
4. Use `forEach` + nested selects or FHIRPath `resolve()` for cross-resource joins.

## References

- HAIStack view engine: `pkg/view/README.md`
- Analytics pipeline: `pkg/analytics/README.md`
- HL7 SQL-on-FHIR IG: https://build.fhir.org/ig/HL7/fhir-analytics/
