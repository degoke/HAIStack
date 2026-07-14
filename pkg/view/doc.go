// Package view implements haistack-view, a standalone Go library for executing
// FHIR ViewDefinition resources into structured JSON rows.
//
// # Scope
//
// v1 is intentionally narrow so the package is useful on its own while remaining
// composable with the rest of Health AI Stack:
//
//   - Single-resource views only.
//   - Scan-based execution via store.ResourceStore.ListIDs and Read.
//   - View-local FHIRPath filters and flat column extraction.
//   - JSON row output.
//   - Optional pluggable authorization and audit hooks.
//   - No joins, nested projections, materialization, incremental refresh, or
//     warehouse sinks.
//
// # ViewDefinition support
//
// The canonical input is a FHIR ViewDefinition resource serialized as JSON. v1
// supports a constrained executable subset:
//
//   - One source resource type per view (the top-level "resource" field).
//   - A single root select containing a flat list of columns.
//   - Each column has a stable output name and a FHIRPath expression.
//   - Optional root filters expressed as FHIRPath predicates in "where" clauses.
//   - Declared permissions as a top-level "permissions" array; this is a v1
//     extension used by Authorizer; auth.ViewAuthorizer is the stack adapter.
//   - Unsupported constructs such as nested selects, forEach, forEachOrNull,
//     unionAll, and materialization directives are rejected at parse time.
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
// resource type, evaluates the compiled filter FHIRPath for each resource,
// extracts the columns for matching resources, and returns rows.
//
// Result.Total is the number of matching resources across the entire scan, not
// just the returned page. NextOffset is set when additional rows are available.
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
//     future installers can register packaged ViewDefinition resources into a
//     view registry.
//   - haistack-ai: consumes Result.Rows directly as structured context.
//   - haistack-auth: auth.ViewAuthorizer implements Authorizer.
//   - haistack-analytics: can reuse the same Executor before materialization
//     exists.
//   - store.MaterializedViewStore: v1 does not refresh materialized views, but
//     ViewSpec and Result metadata are shaped so a future materializer can
//     reuse the same parsed definitions.
package view
