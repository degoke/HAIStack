// Package view implements haistack-view, a standalone Go library for executing
// FHIR ViewDefinition resources into structured JSON rows.
//
// # Scope
//
// The package executes SQL-on-FHIR ViewDefinition projections in-process:
//
//   - Single root resource type per view.
//   - Scan-based execution via store.ResourceStore.ListIDs and Read.
//   - Flat and nested selects with forEach, forEachOrNull, unionAll, and
//     reference joins resolved through the resource store.
//   - JSON row output with row-level pagination for expanded results.
//   - Optional pluggable authorization, audit hooks, and materialized view persistence.
//
// # ViewDefinition support
//
// The canonical input is a FHIR ViewDefinition resource serialized as JSON. Supported
// constructs include:
//
//   - One source resource type per view (the top-level "resource" field).
//   - Root and nested select trees with column, select, forEach, forEachOrNull,
//     and unionAll blocks.
//   - Optional root filters expressed as FHIRPath predicates in "where" clauses.
//   - Materialization metadata: metadata.materialize and metadata.materializeKey.
//   - Declared permissions as a top-level "permissions" array; auth.ViewAuthorizer
//     is the stack adapter.
//
// # Public API
//
// The package centers on three public types:
//
//   - ParseDefinition / DefinitionParser: load a ViewDefinition JSON payload and
//     produce a normalized ViewSpec. A FHIRPath engine is required so that
//     filter and column expressions are validated at parse time.
//   - Registry: in-memory store for named/versioned ViewSpec values. Registering
//     a view compiles its expressions immediately.
//   - Executor: runs a registered view against a store.ResourceStore and returns
//     structured rows.
//
// Typical usage:
//
//	engine, err := fhirpath.NewEngine(fhirpath.Config{})
//	if err != nil { /* handle */ }
//
//	reg := view.NewRegistry()
//	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
//	    // handle
//	}
//
//	exec, err := view.NewExecutor(view.Config{
//	    Resources: resources,
//	    Engine:    engine,
//	    Registry:  reg,
//	})
//	if err != nil { // handle
//	}
//
//	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view"})
//
// # Execution model
//
// Execute resolves the view, applies the optional authorizer, scans the source
// resource type, evaluates filter FHIRPath for each resource, expands the select
// tree into zero or more rows per resource, and returns rows.
//
// Result.Total is the number of output rows across the entire scan, not just the
// returned page. NextOffset is set when additional rows are available.
//
// # Authorization and audit
//
// If the Executor is configured with an Authorizer and the view declares
// permissions, AuthorizeView is called before any resources are read. If the
// Executor is configured with an AuditLogger, LogViewAccess is called on
// success, denial, and view resolution errors. Both seams are optional; the
// package is fully usable without them.
//
// # Integration points
//
//   - haistack-modules: views are declared by name in module metadata today;
//     installers register packaged ViewDefinition resources into a view registry.
//   - haistack-ai: consumes Result.Rows directly as structured context.
//   - haistack-auth: auth.ViewAuthorizer implements Authorizer.
//   - haistack-analytics: reuses the same Executor for reporting-table refresh.
//   - store.MaterializedViewStore: optional persistence when metadata.materialize
//     is set and Executor.Config.MaterializedViews is wired.
package view
