# haistack-view (`pkg/view`)

Execute FHIR `ViewDefinition` resources into structured JSON rows for AI context,
analytics, and permissioned data access.

## What it does

A FHIR `ViewDefinition` is a declarative description of a tabular or structured
projection over one FHIR resource type. This package turns those definitions into
runnable views:

- Load a `ViewDefinition` JSON payload and validate it against the v1 executable
  subset.
- Register named/versioned views in an in-memory registry.
- Execute a view against a `store.ResourceStore` by scanning resources, applying
  FHIRPath filters, and extracting columns.
- Return stable JSON rows with pagination metadata.
- Optionally enforce permissions and write audit records.

In short: **given a ViewDefinition and a resource store, produce structured JSON
rows.**

## Usage

**Register and execute a built-in view:**

```go
engine, err := fhirpath.NewEngine(fhirpath.Config{})
if err != nil { /* handle */ }

reg := view.NewRegistry()
if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
    // handle
}

exec, err := view.NewExecutor(view.Config{
    Resources: resourceStore,
    Engine:    engine,
    Registry:  reg,
})
if err != nil { // handle }

res, err := exec.Execute(ctx, view.ExecuteRequest{
    ViewName: "patient_summary_view",
    Limit:    10,
})
if err != nil { // handle }

for _, row := range res.Rows {
    // row["id"], row["given"], row["family"], ...
}
```

**Custom view with a filter:**

```go
def := []byte(`{
    "resourceType": "ViewDefinition",
    "name": "female_patients",
    "version": "1.0.0",
    "resource": "Patient",
    "select": [{
        "column": [
            {"name": "id", "path": "Patient.id"},
            {"name": "name", "path": "Patient.name.first().family"}
        ]
    }],
    "where": [
        {"path": "Patient.gender = 'female'"}
    ]
}`)

if _, err := reg.Register(def, engine); err != nil {
    // handle
}
```

**Authorization and audit:**

```go
exec, err := view.NewExecutor(view.Config{
    Resources:  resourceStore,
    Engine:     engine,
    Registry:   reg,
    Authorizer: myAuthorizer,
    Audit:      view.AuditStoreAdapter{Store: auditStore},
})
```

If the view declares `permissions`, the authorizer is called before any resources
are read. The audit logger is invoked on success, denial, and resolution errors.

## Where it fits

| Layer | Role |
|-------|------|
| **search** | Find which resources match |
| **fhirpath** | Read fields inside a resource you already have |
| **view** | Build structured projections from resources (this package) |
| **ai** | Consume `Result.Rows` as LLM context |
| **analytics** | Reuse views before materialization exists |

## Supported ViewDefinition subset

- One source resource type per view (`resource`).
- One root select with a flat list of columns.
- Each column has a name and a FHIRPath expression.
- Optional root filters (`where`) expressed as FHIRPath predicates.
- Declared permissions as a top-level `permissions` array (v1 extension).

Unsupported constructs are rejected at parse time:

- Multiple root selects
- Nested selects
- `forEach` / `forEachOrNull`
- `unionAll`
- Joins and materialization directives

## Row encoding

- Empty FHIRPath result → `null` (or `[]` when `collection: true`)
- Singleton scalar → JSON scalar (or one-element array when `collection: true`)
- Multi-item result → JSON array; scalar columns without `collection: true` return `ErrRowEncoding`
- Google FHIR `Date`, `DateTime`, `Time`, and `Instant` protos → FHIR string literals
- FHIR choice wrappers (for example `Observation.effective`) unwrap to their set branch
- `system.Quantity` → `{value, unit, system, code}`
- Proto primitive wrappers → JSON scalar
- Unsupported complex objects → `ErrRowEncoding`

## MVP limits

- Single-resource views only; no joins.
- Scan-based execution through `ListIDs` / `Read`; no search-driven filtering.
- No materialization, incremental refresh, or warehouse sinks.
- No parameter substitution in v1 (`ExecuteRequest.Parameters` is passed to
  auth and audit only).
- In-memory registry only; persistent view registries are future work.

See [doc.go](./doc.go) for the full API, package boundaries, and integration
points.
