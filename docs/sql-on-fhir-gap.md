# SQL-on-FHIR gap analysis (HAIStack ViewDefinition v1)

This document maps the HAIStack ViewDefinition subset implemented in `pkg/view` to the [HL7 SQL-on-FHIR](https://build.fhir.org/ig/HL7/fhir-analytics/) analytics specification.

## Supported in v1

| SQL-on-FHIR concept | HAIStack status | Notes |
|---------------------|-----------------|-------|
| Single resource `select` | Supported | One root resource type per view |
| Flat column projections | Supported | FHIRPath expressions → typed columns |
| `where` filters | Supported | FHIRPath boolean filters |
| View name + version | Supported | Registry key `name\|version` |
| Packaged built-in views | Supported | `patient_summary_view`, `appointment_view`, `observation_view` |
| Incremental refresh (`_since`) | Partial | `LastUpdated` filter on resource envelope; not full SQL-on-FHIR watermark spec |
| IG ViewDefinition install | Partial | `packages.Installer` registers `ViewDefinition` resources into `view.Registry` when analytics is wired |

## Not supported (deferred)

| SQL-on-FHIR concept | HAIStack status | Priority |
|---------------------|-----------------|----------|
| `forEach` / nested selects | Rejected at parse | **P1** — needed for nested structures without flattening |
| `unionAll` | Rejected at parse | P2 — multi-source views |
| Joins across resource types | Rejected at parse | P2 — requires join planner + search/index integration |
| Materialized view directives | Rejected at parse | P2 — `store.MaterializedViewStore` exists but view engine does not populate it |
| Arbitrary SQL backend | Out of scope | By design — HAIStack executes views in-process, not via SQL engine |
| Partitioned output / lakehouse sinks | Stub interfaces only | P3 — `ParquetWarehouseAdapter` seam for cloud mode |

## Portability story

1. **Today:** Ship views as JSON ViewDefinition resources in FHIR NPM packages; `packages.Installer` + `view.RegisterViewDefinition` loads conformant single-select views.
2. **Next:** Expand parser for `forEach` on one nested collection before general joins.
3. **Later:** Evaluate join support against search index coverage rather than full-table scans.

## Validation strategy

- Reject unsupported constructs at `ParseDefinition` time (fail fast).
- Add portable view fixtures from public IGs once `forEach` lands.
- Keep analytics execution on read replica when configured to reduce OLTP impact.

## References

- HAIStack view subset: `pkg/view/README.md`
- Analytics pipeline: `pkg/analytics/README.md`
- HL7 SQL-on-FHIR IG: https://build.fhir.org/ig/HL7/fhir-analytics/
