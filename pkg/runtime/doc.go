// Package runtime implements haistack-runtime, the composition and lifecycle
// library for Health AI Stack. It wires persistence, registry, modules, core,
// search, sync, and HTTP into three explicit deployment modes and owns startup,
// background workers, managed HTTP serving, and deterministic shutdown.
//
// haistack-runtime is library-only. It does not ship a CLI or process supervisor.
// Callers construct a Builder, call Build to open stores and wire services, then
// Start and Shutdown to manage the runtime lifecycle.
//
// # Deployment modes
//
// The effective mode is inferred and validated from builder selections at Build time:
//
//   - ModeLocalSQLite — embedded SQLite as the only durable store. External blob,
//     search, and warehouse adapters are rejected.
//   - ModeEdgePostgresAllInOne — one Postgres tenant for resources, history,
//     search, jobs, modules, audit, blobs, and sync/event stores. Uses internal
//     Postgres-backed stores, not external services.
//   - ModeCloudPostgresPlusExternalServices — Postgres for canonical resource,
//     history, event, and module state plus explicitly injected external adapters
//     for blob storage, search, and/or analytics warehouses.
//
// One Postgres runtime instance serves one tenant. WithPostgresAllInOne requires
// a non-empty tenant ID.
//
// # Builder API
//
// Construct a runtime with the fluent builder:
//
//	rt, err := runtime.New().
//	    WithSQLite("/data/haistack.db").
//	    WithSearch().
//	    WithModules("modules/core").
//	    WithHTTP(":8080").
//	    Build(ctx)
//
// Builder methods:
//
//   - New — returns a new Builder
//   - WithSQLite(path) — select embedded SQLite storage
//   - WithPostgresAllInOne(dsn, tenantID) — select single-tenant Postgres
//   - WithExternalBlobStore, WithExternalSearch, WithExternalWarehouse — cloud adapter seams
//   - WithFHIRPath(engine) — optional; a default engine is created when omitted
//   - WithSDC(service) — optional SDC HTTP operation adapter; the default
//     core/FHIRPath adapter is used when omitted
//   - WithSearch — enable search indexing and query execution
//   - WithSync(hubURL), WithSyncHub(hub), WithSyncNode(nodeID) — device sync
//   - WithModules(paths...) — install modules from local filesystem directories
//   - WithHTTP(addr) — optional managed HTTP server listen address
//   - Build(ctx) — open stores, migrate, wire services, return *Runtime
//
// Build validation rules:
//
//   - Exactly one storage root: SQLite or Postgres, not both
//   - Postgres modes require a tenant ID
//   - local-sqlite rejects external blob/search/warehouse adapters
//   - cloud mode is selected when Postgres is combined with any external adapter
//   - Sync requires WithSync hub URL or WithSyncHub
//
// # Runtime lifecycle
//
//   - Start(ctx) — start background job workers and the managed http.Server when
//     WithHTTP was configured. Returns ErrAlreadyStarted on repeated calls.
//   - Shutdown(ctx) — stop HTTP, cancel background workers, close adapters and DB
//     connections. Repeated calls are safe.
//   - Mode, Config, Services, Handler — accessors for embedding and testing
//   - HTTPAddr — bound listen address after Start when HTTP is configured
//   - SyncEngine, ReindexWorker — optional capability accessors
//
// Shutdown order is deterministic:
//
//  1. Stop HTTP accept loop
//  2. Stop background workers (search reindex and sync job processors)
//  3. Close external adapters implementing CloseableAdapter
//  4. Close database connections last
//
// Build failures roll back partially opened resources via an internal cleanup stack.
//
// # Service graph
//
// Build wires the following through ServiceContainer:
//
//   - registry.Manager with bundled R4 definitions seeded before module install
//   - modules.Manager for filesystem module directories (pkg/modules loader)
//   - fhirpath.Engine for registry-driven search indexing
//   - core.ResourceService with validator, indexer, and outbox as available
//   - search.Service when WithSearch is enabled
//   - sync.Engine when sync is configured
//   - FHIR HTTP handler via pkg/http adapters (always built for Handler access)
//
// ServiceContainer fields:
//
//   - RegistryManager, RegistrySnapshot, ModuleManager
//   - ResourceService, SearchService, SyncEngine, FHIRPathEngine
//   - TenantDB (Postgres modes only)
//   - BlobStore, ExternalSearch, Warehouse (cloud mode adapters)
//
// # Search behavior by mode
//
// WithSearch on SQLite enables embedded/basic search wiring using the local
// SearchStore executor. Advanced registry-driven search and background reindex
// workers are Postgres-first per pkg/search; reindex jobs run only in Postgres
// modes when WithSearch is enabled.
//
// # Sync behavior by mode
//
// SQLite with sync configured acts as a device node: local outbox, cursor, inbox,
// resource, and history stores plus a remote hub (HTTP via WithSync or a direct
// WithSyncHub adapter such as sync.PostgresHub).
//
// Postgres edge mode does not start a device sync engine unless sync is explicitly
// configured. The runtime owns hub-side stores on Postgres but does not schedule
// device push unless WithSync or WithSyncHub is set.
//
// # Cloud adapter seams
//
// BlobStoreAdapter, ExternalSearchAdapter, and WarehouseAdapter are minimal
// runtime-facing interfaces for future cloud integrations. Concrete provider
// implementations live outside pkg/runtime. Adapters need only the methods
// required for dependency registration and future wiring in this phase.
//
// Adapters may implement CloseableAdapter to participate in Shutdown.
//
// # Background workers
//
// When enabled, the runtime owns one background job loop that polls:
//
//   - search.ReindexWorker via jobs.Runner (Postgres + WithSearch)
//   - sync.JobProcessor for retry push, scheduled pull, conflict, and replay jobs
//
// Workers start on Start and stop on Shutdown context cancellation.
//
// # Ownership boundaries
//
// haistack-runtime owns composition, mode inference, lifecycle, and the managed
// HTTP server. It does not own:
//
//   - FHIR resource business rules (pkg/core)
//   - Registry definition ingestion logic (pkg/registry)
//   - Module manifest parsing (pkg/modules)
//   - Search query planning and execution (pkg/search)
//   - Sync protocol details (pkg/sync)
//   - HTTP routing and OperationOutcome mapping (pkg/http)
//   - SQL migrations and store implementations (pkg/sqlite, pkg/postgres)
//
// # File layout
//
//   - doc.go — package documentation
//   - mode.go — Mode constants
//   - config.go — normalized Config struct
//   - builder.go — Builder and mode resolution
//   - runtime.go — Runtime lifecycle (Build, Start, Shutdown)
//   - wire.go — store open/migrate and service wiring
//   - services.go — ServiceContainer
//   - adapters.go — cloud adapter seams
//   - sync_hub.go — HTTP hub adapter for WithSync
//   - indexer_bridge.go — search.Indexer to sync.SearchIndexer bridge
//   - errors.go — validation and lifecycle errors
package runtime
