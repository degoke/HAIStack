# haistack-view (`pkg/view`)

Execute FHIR `ViewDefinition` resources into structured JSON rows for AI context,
analytics, and permissioned data access.

## What it does

A FHIR `ViewDefinition` is a declarative description of a tabular or structured
projection over FHIR data. This package turns those definitions into runnable views:

- Load a `ViewDefinition` JSON payload and validate it.
- Register named/versioned views in an in-memory registry.
- Execute a view against a `store.ResourceStore` by scanning resources, applying
  FHIRPath filters, and expanding nested selects (`forEach`, `unionAll`, joins).
- Return stable JSON rows with pagination metadata.
- Optionally enforce permissions, write audit records, and persist materialized rows.

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

**Nested `forEach` (one row per phone):**

```go
def := []byte(`{
    "resourceType": "ViewDefinition",
    "name": "patient_phones_foreach",
    "version": "1.0.0",
    "resource": "Patient",
    "select": [{
        "column": [{"name": "id", "path": "Patient.id"}],
        "select": [{
            "forEach": "Patient.telecom.where(system = 'phone')",
            "column": [{"name": "phone", "path": "value"}]
        }]
    }]
}`)
```

**Reference join (Appointment → Patient):**

```go
def := []byte(`{
    "resourceType": "ViewDefinition",
    "name": "appointment_patient_join",
    "version": "1.0.0",
    "resource": "Appointment",
    "select": [{
        "column": [{"name": "appt_id", "path": "Appointment.id"}],
        "select": [{
            "forEach": "Appointment.participant.actor",
            "select": [{
                "column": [
                    {"name": "patient_id", "path": "Patient.id"},
                    {"name": "family", "path": "Patient.name.first().family"}
                ]
            }]
        }]
    }]
}`)
```

**Materialized rows:**

```go
exec, err := view.NewExecutor(view.Config{
    Resources:         resourceStore,
    Engine:            engine,
    Registry:          reg,
    MaterializedViews: materializedViewStore,
})
// View metadata: {"materialize": "true", "materializeKey": "id"}
res, err := exec.Execute(ctx, view.ExecuteRequest{
    ViewName: "patient_summary_materialized",
})
```

## Where it fits

| Layer | Role |
|-------|------|
| **search** | Find which resources match |
| **fhirpath** | Read fields inside a resource you already have |
| **view** | Build structured projections from resources (this package) |
| **ai** | Consume `Result.Rows` as LLM context |
| **analytics** | Refresh reporting tables from the same executor |

## Supported ViewDefinition subset

- One source resource type per view (`resource`).
- Root `select` array (multiple entries cross-join).
- Nested `select`, `forEach`, `forEachOrNull`, and `unionAll`.
- Column paths prefixed with a resource type (for example `Patient.id`) evaluate against the root resource; relative paths evaluate against the current `forEach` item.
- Typed relative references in `forEach` collections resolve through the resource store for join-like projections.
- Optional root filters (`where`) expressed as FHIRPath predicates.
- Materialization via `metadata.materialize` / `metadata.materializeKey` when `MaterializedViews` is configured.
- Declared permissions as a top-level `permissions` array (v1 extension).

## SQL-on-FHIR operations

| Operation | Endpoint | Notes |
|-----------|----------|-------|
| `$viewdefinition-run` | `POST /fhir/ViewDefinition/$viewdefinition-run` or `POST /fhir/$viewdefinition-run` | Sync JSON/CSV/NDJSON/Parquet-compatible JSON output |
| `$viewdefinition-export` | `POST /fhir/ViewDefinition/$viewdefinition-export` or `POST /fhir/$viewdefinition-export` | Async export with watermark-aware `_since`; download at `$viewdefinition-export/files/{jobId}/{filename}` |
| `$materialize` | `POST /fhir/ViewDefinition/$materialize` | Async materialized view refresh |
| `$sqlquery-run` | `POST /fhir/Library/$sqlquery-run` or `POST /fhir/$sqlquery-run` | Read-only SQL over reporting tables (Postgres analytics mode) |

## Search-driven execution

Views can declare index-backed prefilters:

```json
"metadata": {
  "searchParams": "status=final",
  "searchMode": "auto"
}
```

When `Executor.Config.Search` is wired, candidate IDs come from the search index. FHIRPath `where` filters still apply as a residual check. `_since` on execute adds `_lastUpdated=gt...` to the search query.

## FHIR `$materialize` operation

```
POST /fhir/ViewDefinition/$materialize
Prefer: respond-async

GET /fhir/ViewDefinition/$materialize/status/{jobId}
```

Requires `ViewMaterializeService` and `MaterializedViews` on the executor (wired in Postgres analytics mode).

## FHIRPath `resolve()` and `memberOf()`

Configure the FHIRPath engine with `Resolve` and `Terminology` (runtime wiring does this automatically when resource store / terminology service are available).

## Row encoding

- Empty FHIRPath result → `null` (or `[]` when `collection: true`)
- Singleton scalar → JSON scalar (or one-element array when `collection: true`)
- Multi-item result → JSON array; scalar columns without `collection: true` return `ErrRowEncoding`
- Google FHIR `Date`, `DateTime`, `Time`, and `Instant` protos → FHIR string literals
- FHIR choice wrappers (for example `Observation.effective`) unwrap to their set branch
- `system.Quantity` → `{value, unit, system, code}`
- Proto primitive wrappers → JSON scalar
- Unsupported complex objects → `ErrRowEncoding`

## Limits

- Reference resolution supports typed, absolute URL, URN, and contained `#` references (including FHIRPath `resolve()` when the evaluation resource is in context).
- `$sqlquery-run` executes read-only SQL against refreshed reporting tables (requires Postgres analytics wiring).
- `$viewdefinition-run` and `$viewdefinition-export` are available on SQLite/edge runtimes when the job store and view executor are wired; inline ViewDefinitions use the same auth/audit path as registered views.
- Parquet export format is a haistack-parquet-v1 JSON envelope, not Apache Parquet binary.
- Search-driven execution requires search wiring; `searchMode=index` fails without index.
- `ExecuteRequest.Parameters` is passed to auth and audit only (no FHIRPath substitution yet).

See [doc.go](./doc.go) for the full API, package boundaries, and integration
points.
