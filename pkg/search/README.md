# haistack-search (`pkg/search`)

Registry-driven FHIR search for the haistack monorepo.

## What it does

**haistack-search** is the **FHIR search library** for HAIStack. It turns registry `SearchParameter` definitions into typed index rows, parses FHIR query strings, plans lookups against those rows, and returns bundle-ready results.

Think of it as two pipelines:

| Pipeline | What happens |
|----------|--------------|
| **Write path (indexing)** | Read enabled `SearchParameter` metadata from `pkg/registry`, evaluate FHIRPath expressions on each resource, normalize values into typed field keys, emit `store.SearchIndexEntry` rows |
| **Read path (search)** | Parse query params → resolve against registry → build a lookup plan → execute via `store.SearchQueryExecutor` → load resources and assemble a search bundle |

Main components:

| Component | Role |
|-----------|------|
| `SnapshotRegistry` | Wraps a registry snapshot; exposes all registry-backed search parameters |
| `RegistryIndexer` | `search.Indexer` for `pkg/core` write-path indexing |
| `ParseQuery` / `ResolveQuery` / `BuildPlan` | Parse FHIR params and plan typed index lookups |
| `Planner` / `NewPlanner` | Default plan builder used by `Service` |
| `StoreExecutor` | Execute plans via `store.SearchQueryExecutor` + sort/pagination |
| `Service` | High-level entrypoint: `Search`, `SearchRequest`, `SearchBundle` |
| `ReindexWorker` / `ReindexJobRunner` / `ReindexNotifier` | Rebuild index rows and enqueue jobs on registry changes |

It implements search **logic** on top of **`pkg/store` contracts**. Backends (`pkg/postgres`, `pkg/sqlite`) persist index rows and run lookups; this package does not open databases directly.

It does **not**:

- Parse raw FHIR JSON or assign version IDs (`pkg/types`, `pkg/core`)
- Store resources or run SQL (`pkg/postgres`, `pkg/sqlite`)
- Serve HTTP search endpoints (`pkg/http` delegates here)
- Compile or install registry definitions (`pkg/registry`)

## How it fits in the ecosystem

```
  pkg/registry (Snapshot / SearchParameter metadata)
           |
           v
  SnapshotRegistry  +  pkg/fhirpath (expression evaluation)
           |
     +-----+-----+
     v           v
 RegistryIndexer   ParseQuery / ResolveQuery / BuildPlan
 (write path)              |
     |                     v
     v              StoreExecutor / Service (read path)
 store.SearchStore          |
     ^                      v
     |              store.ResourceStore (load hits)
 pkg/core (Indexer hook)     |
     |                      v
 pkg/postgres / pkg/sqlite   bundle-ready Result / SearchBundle
 (typed index tables)              |
                                   v
                            pkg/http SearchServiceAdapter
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **registry** | Enabled resource types, search parameter codes, FHIRPath expressions |
| Upstream | **fhirpath** | Evaluate expressions during indexing |
| Upstream | **store** | `SearchStore`, `SearchQueryExecutor`, `ResourceStore` contracts |
| Downstream | **core** | Calls `Indexer.Build` on accepted writes |
| Downstream | **http** | Type-level GET/POST search via `SearchService` adapter |
| Downstream | **subscriptions** | `ParseQuery` + `MatchResourceParameter` for FHIR `Subscription.criteria` |
| Sidecar | **postgres** / **sqlite** | Index persistence and lookup execution |

## When to use it

- Indexing resources on create/update/delete through `pkg/core`
- Executing FHIR search queries against Postgres-backed typed indexes
- Rebuilding search indexes after registry or SearchParameter changes
- Tests that need real parse/plan/execute behavior without HTTP
- Matching resources against simple search criteria inside subscription triggers

## Usage modes

### 1. Registry snapshot for parameters and indexing

Wrap a compiled registry snapshot before wiring indexing or search:

```go
import (
    "github.com/degoke/haistack/pkg/registry"
    "github.com/degoke/haistack/pkg/search"
)

snapshot, _ := manager.RebuildSnapshot(ctx)
reg := search.NewSnapshotRegistry(snapshot)
params := reg.SearchParametersFor("Patient")
enabled := reg.EnabledResourceTypes()
```

### 2. Write-path indexing in `pkg/core`

```go
import (
    "github.com/degoke/haistack/pkg/core"
    "github.com/degoke/haistack/pkg/fhirpath"
)

engine := fhirpath.NewEngine(/* … */)
indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
    Registry: reg,
    Engine:   engine,
})

svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Resources: tdb.ResourceStore(),
    History:   tdb.HistoryStore(),
    Sessions:  tdb,
    Indexer:   indexer,
})
```

On each accepted write, core calls `Indexer.Build` and persists the returned entries through `SearchStore`.

### 3. High-level search execution (`Service`)

```go
import "net/url"

searchSvc, err := search.NewService(search.ServiceConfig{
    Registry:  reg,
    Executor:  search.NewStoreExecutor(tdb.SearchStore(), tdb.ResourceStore()),
    Resources: tdb.ResourceStore(),
    BaseURL:   "https://example.com/fhir", // optional paging links
})

params := url.Values{}
params.Set("name", "Smith")
result, err := searchSvc.Search(ctx, "Patient", params)
bundle, err := searchSvc.SearchBundle(ctx, "Patient", params)
```

`ServiceConfig.Planner` is optional; when nil, `NewPlanner()` is used.

### 4. Low-level parse, resolve, and plan (tests and tooling)

```go
parsed, err := search.ParseQuery("Patient", params)
resolved, err := search.ResolveQuery(reg, "Patient", parsed)
plan, err := search.BuildPlan(reg, "Patient", params)
execResult, err := executor.Execute(ctx, plan)
```

Use this path when you need to inspect intermediate structures or inject a custom `Executor` without `Service`.

### 5. Reindex after registry changes

```go
manager := registry.NewManager(registry.Config{
    Definitions:   db.DefinitionStore(),
    Installs:      tdb.RegistryInstallStore(),
    SearchReindex: search.NewReindexNotifier(tdb.JobStore()),
})

worker := &search.ReindexWorker{
    Registry:  reg,
    Indexer:   indexer,
    Resources: tdb.ResourceStore(),
    Search:    tdb.SearchStore(),
}
runner := &search.ReindexJobRunner{Jobs: tdb.JobStore(), Worker: worker}
processed, err := runner.RunOnce(ctx)
```

`EnableResource` and `InstallDefinition` on the registry manager enqueue reindex jobs when `SearchReindex` is configured.

### 6. Parameter discovery for UIs and validators

```go
params := searchSvc.SearchParametersFor("Patient") // []search.ParameterInfo
enabled := searchSvc.EnabledResourceTypes()
```

Unknown parameter codes return `search.UnknownParamError` with a structured `Code` field (`search.UnknownParamCode`).

## Examples

**Chained and advanced query (Postgres backend):**

```go
params := url.Values{}
params.Set("subject.name", "Smith")
params.Set("_include", "Observation:subject")
params.Set("_sort", "-date")
params.Set("_count", "25")
result, err := searchSvc.Search(ctx, "Observation", params)
```

**Count-only summary:**

```go
params := url.Values{}
params.Set("_summary", "count")
result, err := searchSvc.Search(ctx, "Patient", params)
// result.Total set, result.Entries empty
```

**Match a resource against one search parameter (subscriptions-style):**

```go
matched, known := search.MatchResourceParameter(ctx, reg, engine, "Patient", envelope, "active", []string{"true"})
```

## Supported search features

Postgres-first advanced FHIR search:

| Feature | Status |
|---------|--------|
| Registry-backed parameters | All installed SearchParameters for enabled resource types |
| `_count` / `_offset` | Paging with max `_count` of 100 |
| `_sort` | Registry-backed fields plus `_id` and `_lastUpdated` |
| Modifiers | `string:exact`, `string:contains`; `uri:below`, `uri:above`; token/reference modifiers per type |
| Prefixes | Date/number comparators: `eq`, `ne`, `gt`, `ge`, `lt`, `le`, `sa`, `eb`, `ap` |
| Chained search | Up to two hops (e.g. `subject.name`, `subject.organization.name`) |
| Reverse chaining | `_has:Type:ref:param` (one extra nested `_has`) |
| `_include` / `_revinclude` | Direct includes plus `ResourceType:*` and `*:*` wildcards |
| Composite search | Declared composite SearchParameters from registry |
| `_summary` / `_elements` | Response projection at assembly time |
| Full text | Postgres native FTS via indexed `text.*` documents |
| Reindexing | Background jobs on registry SearchParameter changes |

Unsupported semantics return explicit errors (`ErrUnsupportedFeature`, `ErrInvalidQuery`).

## Query semantics

- Repeated parameters **AND** together (`?name=Smith&birthdate=1980-01-01`)
- Comma-separated values **OR** within one occurrence (`?name=Smith,Jones`)
- `_count` and `_offset` apply to primary matches only (not included resources)
- `_sort` uses registry metadata; tiebreak on resource id
- Chain depth is limited to 2 hops; `_include:iterate` remains unsupported

## Index field keys

`RegistryIndexer` normalizes extracted values into typed keys consumed by `store.SearchStore`:

| Prefix | Example key | Stored as |
|--------|---------------|-----------|
| `token.*` | `token.status` | Code/system/value tokens |
| `string.*` | `string.family` | Normalized strings |
| `date.*` | `date.birthdate` | Comparable date strings |
| `reference.*` | `reference.patient` | Reference targets (typed/id/canonical forms) |
| `uri.*` | `uri.url` | Canonical/URI strings (stored with string indexes) |
| `composite.*` | `composite.context-type-value` | Composite component values |
| `text.*` | `text.document` | Postgres full-text document |

See [`pkg/sqlite`](../sqlite/README.md#search-index-field-keys) and [`pkg/postgres`](../postgres/README.md) for how backends route keys to tables.

## Configuration / key types

| Type | Role |
|------|------|
| `Registry` | Search parameter metadata and enablement |
| `Indexer` | Interface implemented by `RegistryIndexer` |
| `Executor` | Plan execution (`StoreExecutor`) |
| `Planner` | `PlanSearch` from registry + params |
| `ServiceConfig` | `Registry`, `Executor`, `Planner`, `Resources`, `BaseURL` |
| `Request` | Structured search input for `SearchRequest` |
| `Result` | Entries, total, links, summary mode |

**Sentinel errors:** `ErrUnknownParam`, `ErrUnsupportedParam`, `ErrUnsupportedFeature`, `ErrInvalidQuery`, `ErrResourceTypeDisabled`, `ErrProjectionFailed`.

## Where it fits

| Layer | Role |
|-------|------|
| **registry** | Bundled SearchParameters and enablement snapshot |
| **fhirpath** | Expression evaluation for indexing |
| **search** | Index extraction, query parse/plan/execute, reindex |
| **store** | Search index persistence and lookup contracts |
| **postgres** | Primary execution backend (`LookupMatch`, `FieldValues`) |
| **sqlite** | Index persistence + lookups for tests and embedded nodes |
| **core** | Write pipeline; plugs in `search.Indexer` |
| **http** | REST search endpoints via adapter |

## Limits

- Postgres is the primary complete execution backend; SQLite stores indexes and supports basic lookups but not the full advanced execution surface
- Chain depth is limited to 2; recursive `_include:iterate` is deferred
- OpenSearch adapter seam is preserved via `SearchAdvancedExecutor`; not implemented yet
- No HTTP routing in this package — use `pkg/http`
- Custom SearchParameters become searchable after snapshot rebuild and reindex completion
- Max `_count` is 100 at the service layer

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — monorepo layering
- [pkg/registry/README.md](../registry/README.md) — SearchParameter catalog and snapshot compile
- [pkg/fhirpath/README.md](../fhirpath/README.md) — expression engine for indexing
- [pkg/core/README.md](../core/README.md) — write path and `Indexer` hook
- [pkg/store/README.md](../store/README.md) — `SearchStore` and executor contracts
- [pkg/http/README.md](../http/README.md) — FHIR REST search endpoints
- [pkg/postgres/README.md](../postgres/README.md) / [pkg/sqlite/README.md](../sqlite/README.md) — backends
- [doc.go](./doc.go) — full API, error types, and file layout
