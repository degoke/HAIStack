# haistack-search (`pkg/search`)

Registry-driven FHIR search for the health-ai-stack monorepo.

## What it does

**haistack-search** is the **FHIR search library** for Health AI Stack. It turns registry `SearchParameter` definitions into typed index rows, parses FHIR query strings, plans lookups against those rows, and returns bundle-ready results.

Think of it as two pipelines:

| Pipeline | What happens |
|----------|--------------|
| **Write path (indexing)** | Read enabled `SearchParameter` metadata from `pkg/registry`, evaluate FHIRPath expressions on each resource, normalize values into typed field keys, emit `store.SearchIndexEntry` rows |
| **Read path (search)** | Parse query params → resolve against registry → build a lookup plan → execute via `store.SearchQueryExecutor` → load resources and assemble a search bundle |

Main components:

| Component | Role |
|-----------|------|
| `SnapshotRegistry` | Wraps a registry snapshot; exposes only supported search parameters |
| `RegistryIndexer` | `search.Indexer` for `pkg/core` write-path indexing |
| `ParseQuery` / `ResolveQuery` / `BuildPlan` | Parse FHIR params and plan typed index lookups |
| `StoreExecutor` | Execute plans via `store.SearchQueryExecutor` + sort/pagination |
| `Service` | High-level entrypoint: `Search` and `SearchBundle` |
| `ReindexWorker` / `ReindexJobRunner` / `ReindexNotifier` | Rebuild index rows and enqueue jobs on registry changes |

It implements search **logic** on top of **`pkg/store` contracts**. Backends (`pkg/postgres`, `pkg/sqlite`) persist index rows and run lookups; this package does not open databases directly.

It does **not**:

- Parse raw FHIR JSON or assign version IDs (`pkg/types`, `pkg/core`)
- Store resources or run SQL (`pkg/postgres`, `pkg/sqlite`)
- Serve HTTP search endpoints (future API layer)
- Compile or install registry definitions (`pkg/registry`)

## When to use it

- Indexing resources on create/update/delete through `pkg/core`
- Executing FHIR search queries against Postgres-backed typed indexes
- Rebuilding search indexes after registry or SearchParameter changes
- Tests that need real parse/plan/execute behavior without HTTP

## Usage

**Wrap a registry snapshot:**

```go
import (
    "github.com/degoke/health-ai-stack/pkg/registry"
    "github.com/degoke/health-ai-stack/pkg/search"
)

snapshot, _ := manager.RebuildSnapshot(ctx)
reg := search.NewSnapshotRegistry(snapshot)
```

**Wire indexing into `pkg/core`:**

```go
import (
    "github.com/degoke/health-ai-stack/pkg/core"
    "github.com/degoke/health-ai-stack/pkg/fhirpath"
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

**Execute search:**

```go
import "net/url"

searchSvc, err := search.NewService(search.ServiceConfig{
    Registry:  reg,
    Executor:  search.NewStoreExecutor(tdb.SearchStore(), tdb.ResourceStore()),
    Resources: tdb.ResourceStore(),
    BaseURL:   "https://example.com/fhir", // optional, for bundle paging links
})

params := url.Values{}
params.Set("name", "Smith")
result, err := searchSvc.Search(ctx, "Patient", params)
// result.Total, result.Entries, result.Links
```

**Schedule and run reindex jobs:**

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

processed, err := runner.RunOnce(ctx) // claim and run one "reindex" job
```

`EnableResource` and `InstallDefinition` on the registry manager enqueue reindex jobs when `SearchReindex` is configured.

## Supported search parameters

These parameter codes are implemented end-to-end:

| Code | Typical use |
|------|-------------|
| `_id` | Resource identity |
| `_lastUpdated` | Version timestamp |
| `identifier` | Business identifiers |
| `name`, `phone`, `birthdate` | Demographics (Patient) |
| `patient`, `subject`, `encounter` | References |
| `status`, `date`, `code` | Clinical/event filters |

Parameters defined in the registry but outside this set are ignored for indexing and rejected at query time with `ErrUnsupportedParam`.

## Query semantics

- Repeated parameters **AND** together (`?name=Smith&birthdate=1980-01-01`)
- Comma-separated values **OR** within one occurrence (`?name=Smith,Jones`)
- `_count` and `_offset` provide offset paging
- `_sort` supports `_id` and `_lastUpdated`

Explicitly rejected today: chained search, modifiers, prefixes, `_include`, `_revinclude`, `_summary`, `_elements`, full text, and composite search.

## Index field keys

`RegistryIndexer` normalizes extracted values into typed keys consumed by `store.SearchStore`:

| Prefix | Example key | Stored as |
|--------|---------------|-----------|
| `token.*` | `token.status` | Code/system/value tokens |
| `string.*` | `string.family` | Normalized strings |
| `date.*` | `date.birthdate` | Comparable date strings |
| `reference.*` | `reference.patient` | Reference targets |

See [`pkg/sqlite`](../sqlite/README.md#search-index-field-keys) and [`pkg/postgres`](../postgres/README.md) for how backends route keys to tables.

## Mental model

```
pkg/registry  →  SearchParameter metadata + FHIRPath expressions
pkg/fhirpath  →  evaluate expressions on resource JSON
pkg/search    →  normalize → index rows; parse → plan → execute queries
pkg/store     →  SearchStore (write rows) + SearchQueryExecutor (lookups)
pkg/core      →  calls Indexer on write; does not parse search queries
```

**Write path:**

```
ResourceEnvelope  →  RegistryIndexer.Build  →  []SearchIndexEntry  →  SearchStore.Index
```

**Read path:**

```
url.Values  →  ParseQuery  →  ResolveQuery  →  BuildPlan  →  StoreExecutor  →  Result / Bundle
```

**One line:** `pkg/search` is the **search brain** — it knows which parameters exist, how to index them, and how to turn query strings into typed lookups.

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

## Current limits

- Postgres is the primary complete execution backend; SQLite stores indexes and supports lookups but is not required to run full FHIR search in embedded deployments
- Only the supported parameter set above is indexed and queryable
- No HTTP `_search` endpoint in this package
- OpenSearch / full-text search is out of scope
- Chained, composite, modifier, and `_include` features are rejected explicitly rather than silently ignored

See [doc.go](./doc.go) for the full API, error types, and file layout.
