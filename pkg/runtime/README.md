# haistack-runtime (`pkg/runtime`)

Composition and lifecycle glue for the health-ai-stack monorepo.

## What it does

**haistack-runtime** is the **runtime composition library** for Health AI Stack. It turns lower-level packages into a single runnable service graph with managed startup, background workers, optional HTTP serving, and deterministic shutdown.

Think of it as the layer that answers: *which stores, which tenant, which capabilities, and how do they start and stop together?*

| Concern | What runtime owns |
|---------|-------------------|
| **Storage** | Open + migrate SQLite or Postgres; ensure tenant for Postgres |
| **Registry** | Seed bundled R4 definitions; rebuild snapshot after module install |
| **Modules** | Install filesystem module directories via `pkg/modules` |
| **Core** | Wire `core.ResourceService` with validator, indexer, and outbox |
| **Search** | Wire `search.Service` and optional reindex worker (Postgres) |
| **Sync** | Wire `sync.Engine` and job processor when hub is configured |
| **HTTP** | Build FHIR handler; optionally manage `http.Server` lifecycle |
| **Jobs** | Poll reindex and sync background work under one runtime loop |

It does **not**:

- Parse FHIR JSON or implement CRUD rules (`pkg/core`)
- Define store SQL or migrations (`pkg/sqlite`, `pkg/postgres`)
- Serve as a standalone binary (no CLI in this package)
- Implement cloud provider SDKs (adapter seams only)
- Replace manual wiring in tests that need fine-grained control over one subsystem

## When to use it

- Building a local offline-first node (SQLite + optional sync hub)
- Running a single-tenant edge server (Postgres all-in-one)
- Composing a cloud deployment shape with injected external adapters
- Integration tests that need a full stack without copy-pasting wiring from README examples
- Embedding the FHIR handler in a custom HTTP server via `Runtime.Handler()`

Prefer manual wiring when you need only one package (e.g. core-only or search-only unit tests).

## Deployment modes

Modes are **inferred at `Build` time** from builder selections:

| Mode | Storage | External adapters | Typical use |
|------|---------|-------------------|-------------|
| `local-sqlite` | SQLite | Rejected | Offline device, local dev, embedded nodes |
| `edge-postgres-all-in-one` | Postgres (one tenant) | Not used | Edge/server deployment |
| `cloud-postgres-plus-external-services` | Postgres (one tenant) | Required for mode selection | Cloud with S3/OpenSearch/etc. seams |

### Mode selection rules

- Exactly **one** storage backend: `WithSQLite` **or** `WithPostgresAllInOne`, never both
- Postgres requires a **tenant ID**
- Any `WithExternalBlobStore`, `WithExternalSearch`, or `WithExternalWarehouse` with Postgres selects **cloud** mode
- `WithExternal*` adapters on SQLite return `ErrExternalAdapterInSQLite`

## Quick start

### Local SQLite (MVP)

```go
import (
    "context"
    "log"

    "github.com/degoke/health-ai-stack/pkg/runtime"
)

func main() {
    ctx := context.Background()

    rt, err := runtime.New().
        WithSQLite("/data/haistack.db").
        WithSearch().
        WithModules("modules/core").
        WithHTTP(":8080").
        Build(ctx)
    if err != nil {
        log.Fatal(err)
    }

    if err := rt.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer rt.Shutdown(ctx)

  // rt.Handler() is also available for custom servers
}
```

### Edge Postgres (single tenant)

```go
rt, err := runtime.New().
    WithPostgresAllInOne("postgres://user:pass@localhost/haistack?sslmode=disable", "tenant-a").
    WithSearch().
    WithModules("modules/core", "modules/scheduling").
    WithHTTP(":8080").
    Build(ctx)
```

Postgres mode with `WithSearch` also wires a **background reindex worker** and registers `search.NewReindexNotifier` on the registry manager so SearchParameter changes enqueue reindex jobs.

### Cloud Postgres + external adapters

```go
rt, err := runtime.New().
    WithPostgresAllInOne(dsn, "tenant-a").
    WithExternalBlobStore(myBlobAdapter).
    WithExternalSearch(mySearchAdapter).
    WithExternalWarehouse(myWarehouseAdapter).
    WithSearch().
    WithHTTP(":8080").
    Build(ctx)

// Adapters are exposed on the service container:
rt.Services().BlobStore
rt.Services().ExternalSearch
rt.Services().Warehouse
```

Concrete provider implementations belong outside `pkg/runtime`. The adapter interfaces are stable seams for future wiring.

## Builder reference

| Method | Purpose |
|--------|---------|
| `New()` | Create a builder |
| `WithSQLite(path)` | Embedded SQLite database path |
| `WithSQLiteTenant(id)` / `WithSQLiteTerminologyScope(scope)` | Configure local sync and terminology namespaces |
| `WithPostgresAllInOne(dsn, tenantID)` | Single-tenant Postgres |
| `WithExternalBlobStore(adapter)` | Cloud blob store seam |
| `WithExternalSearch(adapter)` | Cloud search seam |
| `WithExternalWarehouse(adapter)` | Cloud analytics/reporting seam |
| `WithFHIRPath(engine)` | Optional FHIRPath engine (default created if omitted) |
| `WithSearch()` | Enable search indexing + query service |
| `WithSync(hubURL)` | Device sync against remote HTTP hub |
| `WithSyncHub(hub)` | Device sync with a pre-built `sync.Hub` (e.g. `sync.PostgresHub`) |
| `WithSyncNode(nodeID)` | Device node ID (default: `runtime-node`) |
| `WithModules(paths...)` | Install local module directories at build time |
| `WithHTTP(addr)` | Managed HTTP listen address (optional) |
| `WithHTTPAuth(...)` / `WithHTTPMiddleware(...)` | Configure managed HTTP authentication and policy middleware |
| `WithHTTPRateLimit(config)` | Configure process-local managed HTTP rate limiting |
| `WithModuleAuthorizer(authorizer)` | Authorize module installs and upgrades |
| `WithModuleVerifier(verifier)` | Verify module signatures/content before install and upgrade |
| `Build(ctx)` | Wire everything; returns `*Runtime` |

## Lifecycle

```
Build(ctx)  →  open DB, migrate, seed registry, install modules, wire services
Start(ctx)  →  background job loop + optional http.Server
Shutdown(ctx)  →  stop HTTP → stop workers → close adapters → close DB
```

| Method | Behavior |
|--------|----------|
| `Build` | Heavy initialization; safe to call without starting HTTP |
| `Start` | Starts workers/server; cancellation of its context stops workers; repeated calls return `ErrAlreadyStarted` |
| `Shutdown` | Safe to call multiple times; respects context deadline |
| `Handler()` | FHIR REST handler always built; usable without `WithHTTP` |
| `HTTPAddr()` | Actual bound address after `Start` (useful with `:0`) |
| `Services()` | `ServiceContainer` with managers and wired services |
| `Mode()` / `Config()` | Effective mode and normalized config |

### Shutdown order

1. Stop HTTP accept loop (`http.Server.Shutdown`)
2. Cancel background job context and wait for goroutines
3. Close `CloseableAdapter` implementations
4. Close SQLite/Postgres connections

Build failures roll back partially opened resources before returning an error.

## Service container

`Runtime.Services()` returns:

| Field | When set |
|-------|----------|
| `RegistryManager` | Always |
| `RegistrySnapshot` | Always (after build) |
| `ModuleManager` | Always |
| `ResourceService` | Always |
| `SearchService` | When `WithSearch()` |
| `SyncEngine` | When sync hub configured |
| `FHIRPathEngine` | Always |
| `TenantDB` | Postgres modes |
| `BlobStore`, `ExternalSearch`, `Warehouse` | Cloud mode adapters |

## Search by mode

| Mode | `WithSearch()` behavior |
|------|-------------------------|
| SQLite | Embedded/basic search via local `SearchStore` executor |
| Postgres | Full search service + background reindex worker |

Advanced FHIR search features (`_include`, chained search, composites, FTS) remain **Postgres-first** per `pkg/search`. SQLite persists index rows and supports basic lookups.

## Sync by mode

| Mode | Sync wiring |
|------|-------------|
| SQLite + `WithSync` / `WithSyncHub` | Device node: local outbox/cursor/inbox + remote hub |
| Postgres (edge) | Hub-side stores available; device engine only when sync explicitly configured |

`WithSync(hubURL)` uses the `pkg/client` sync client over HTTP. `WithSyncHub` accepts any `sync.Hub` implementation, including `sync.PostgresHub` for device-to-Postgres-hub tests.

When search indexing is enabled alongside sync, the runtime bridges `search.Indexer` to `sync.SearchIndexer` so pull-apply can rebuild search rows.

## Cloud adapter seams

Minimal interfaces for provider plugins:

```go
type BlobStoreAdapter interface {
    Name() string
    BlobStore() store.BlobStore
}

type ExternalSearchAdapter interface {
    Name() string
    SearchExecutor() store.SearchQueryExecutor
}

type WarehouseAdapter interface {
    Name() string
    ReportingTables() store.ReportingTableStore
}
```

Implement `CloseableAdapter` when the adapter holds connections that must close on shutdown.

## Errors

| Error | Cause |
|-------|-------|
| `ErrNoStorage` | Neither SQLite nor Postgres configured |
| `ErrConflictingStorage` | Both SQLite and Postgres configured |
| `ErrMissingTenantID` | Postgres without tenant ID |
| `ErrExternalAdapterInSQLite` | External adapter on SQLite |
| `ErrSyncHubRequired` | Sync enabled without hub URL or hub |
| `ErrAlreadyStarted` | Second `Start` call |
| `ErrInvalidModeCombination` | Unsupported dependency set |

## Mental model

```
Builder selections  →  mode inference + validation
        ↓
Open + migrate store(s)
        ↓
Seed registry → install modules → rebuild snapshot
        ↓
FHIRPath + validator + indexer + ResourceService + SearchService + SyncEngine
        ↓
pkg/http handler (+ optional managed Server)
        ↓
Start: job loop + HTTP     Shutdown: reverse order
```

**One line:** `pkg/runtime` is the **wiring and lifecycle layer** — it knows how to assemble the stack for local, edge, and cloud shapes and keep it running.

## Where it fits

| Layer | Role |
|-------|------|
| **runtime** | Composition, mode selection, lifecycle |
| **sqlite / postgres** | Persistence implementations |
| **registry** | FHIR definition catalog |
| **modules** | Manifest-driven installs |
| **core** | Resource CRUD and transactions |
| **search** | Indexing and query execution |
| **sync** | Device-to-hub replication |
| **http** | FHIR REST transport adapter |
| **jobs** | Background job claiming and dispatch |

## Testing

```bash
go test ./pkg/runtime/...
```

Postgres integration tests use testcontainers (`postgres:16-alpine`) or `TEST_POSTGRES_DSN` when Docker is unavailable.

Short mode skips Postgres-dependent tests:

```bash
go test ./pkg/runtime/... -short
```

## Current limits

- Library-only; no `cmd/haistack` CLI in this package
- One Postgres tenant per runtime instance
- Cloud adapters are registration seams; provider SDKs are out of scope here
- HTTP hub sync depends on a remote server exposing `/sync/push` and `/sync/pull`
- Auth middleware and SMART flows are not wired by default (use `Handler()` with custom server middleware)

See [doc.go](./doc.go) for the full package documentation and file layout.
