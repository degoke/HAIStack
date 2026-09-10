# SQL-on-FHIR gap analysis (HAIStack ViewDefinition v2)

This document maps the HAIStack ViewDefinition implementation in `pkg/view` to the [HL7 SQL-on-FHIR](https://build.fhir.org/ig/HL7/fhir-analytics/) analytics specification.

## Supported

| SQL-on-FHIR concept | HAIStack status | Notes |
|---------------------|-----------------|-------|
| Single resource `select` | Supported | One root resource type per view |
| Flat column projections | Supported | FHIRPath expressions → typed columns |
| `where` filters | Supported | FHIRPath boolean filters |
| View name + version | Supported | Registry key `name\|version` |
| Packaged built-in views | Supported | `patient_summary_view`, `appointment_view`, `observation_view` |
| `forEach` / `forEachOrNull` | Supported | Expands nested collections; relative column paths use `%i` focus evaluation |
| Nested `select` (cross join) | Supported | Parent columns preserved via row merge |
| Multiple root `select` entries | Supported | Treated as cross-join siblings |
| `unionAll` | Supported | Branches must declare identical column names |
| Reference joins | Partial | Typed relative references resolved via `store.ResourceStore.Read`; no `resolve()` in FHIRPath |
| Materialized view persistence | Partial | Opt-in via view `metadata.materialize` + `Executor.MaterializedViews`; not the FHIR `$materialize` operation |
| Incremental refresh (`_since`) | Partial | `LastUpdated` filter on resource envelope; not full SQL-on-FHIR watermark spec |
| IG ViewDefinition install | Partial | `packages.Installer` registers conformant views when analytics is wired |

## Not supported (deferred)

| SQL-on-FHIR concept | HAIStack status | Priority |
|---------------------|-----------------|----------|
| FHIR `$materialize` operation | Out of scope | P2 — HAIStack uses in-process materialization hooks instead |
| Arbitrary SQL backend | Out of scope | By design — HAIStack executes views in-process |
| `resolve()` in FHIRPath | Out of scope | Joins use view-engine reference resolution instead |
| Partitioned output / lakehouse sinks | Stub interfaces only | P3 — `ParquetWarehouseAdapter` seam for cloud mode |
| Full SQL-on-FHIR watermark / change detection | Partial | Analytics CDC uses outbox cursors, not IG watermark spec |

## Portability story

1. **Today:** Ship views as JSON ViewDefinition resources in FHIR NPM packages; `packages.Installer` + `view.RegisterViewDefinition` loads views with flat or nested selects.
2. **Joins:** Use `forEach` on reference collections and nested selects; the executor resolves `ResourceType/id` references from the resource store.
3. **Materialization:** Set `metadata.materialize=true` and wire `Executor.Config.MaterializedViews` to persist rows keyed by `metadata.materializeKey` (defaults to `id`).

## Validation strategy

- Reject structurally invalid constructs at `ParseDefinition` time (fail fast).
- `unionAll` branches must expose identical column names in the same order.
- Keep analytics execution on read replica when configured to reduce OLTP impact.

## References

- HAIStack view engine: `pkg/view/README.md`
- Analytics pipeline: `pkg/analytics/README.md`
- HL7 SQL-on-FHIR IG: https://build.fhir.org/ig/HL7/fhir-analytics/
