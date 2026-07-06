// Package sqlite implements haistack-sqlite, the embedded offline database layer for
// health-ai-stack local deployments.
//
// haistack-sqlite is the SQLite-backed implementation of pkg/store contracts for device,
// tablet, workstation, and lightweight edge nodes. It owns schema, migrations, and the
// atomic local write path. This is not bytefhir-sqlite; it is the haistack runtime's
// embedded persistence adapter at github.com/degoke/health-ai-stack/pkg/sqlite.
//
// # Design principles
//
// Single-transaction local writes:
//
//   - A local write must update current resource state, immutable history, sync outbox,
//     and typed search index rows in one SQLite transaction so persistence and sync state
//     cannot drift.
//   - ApplyLocalWrite and Session implement this guarantee for callers.
//   - Version assignment and search extraction stay outside this package; SQLite persists
//     caller-supplied version IDs and prepared SearchIndexEntry values.
//
// Canonical JSON:
//
//   - types.ResourceEnvelope.JSON is the source of truth for resource content.
//   - Stores persist JSON blobs plus metadata columns (version_id, last_updated, hash).
//   - Optional proto blob columns are deferred to a future migration.
//
// Pure Go driver:
//
//   - database/sql with modernc.org/sqlite for cross-platform embedded use without CGO.
//   - Default pragmas: foreign_keys=ON, journal_mode=WAL, configurable busy_timeout.
//
// # Opening a database
//
//	db, err := sqlite.Open("/path/to/haistack.db")
//	if err != nil { … }
//	defer db.Close()
//
//	if err := db.Migrate(ctx); err != nil { … }
//
// Open accepts functional options such as WithBusyTimeout. Use path ":memory:" for
// ephemeral in-memory databases (common in tests).
//
// # Implemented contracts (MVP-complete)
//
// These types satisfy pkg/store interfaces and are fully wired for local MVP use:
//
//   - ResourceStore  → store.ResourceStore   — current-state CRUD
//   - HistoryStore   → store.HistoryStore    — immutable version history and tombstones
//   - SearchStore    → store.SearchStore     — typed search_token/string/date/number/reference tables
//   - OutboxStore    → store.EventStore      — append-only sync_outbox with monotonic sequence
//   - CursorStore    → store.CursorStore     — named sync_cursor checkpoints
//   - Session        → store.WriteSession    — transaction-scoped stores for atomic writes
//   - DB             → store.WriteSessionProvider via BeginWrite
//
// # Scaffold contracts (schema + store; upstream workflow deferred)
//
// These adapters persist data correctly but intentionally omit higher-level orchestration:
//
//   - InboxStore     → store.InboxStore — sync_inbox_applied idempotency for pull/apply.
//   - ConflictStore  → store.ConflictStore — sync_conflict append/list/resolve.
//     Conflict detection and reconciliation policy live in pkg/core and sync layers.
//   - BinaryStore    → store.BinaryStore   — small inline binary_object payloads.
//     Large blob offload and deduplication are out of scope for embedded SQLite.
//   - ModuleStore    → store.ModuleStore   — module_registry metadata.
//     Code loading and activation policy live elsewhere.
//
// # Out of scope (not in haistack-sqlite MVP)
//
// Use pkg/postgres or future adapters for:
//
//   - optional proto column on resource
//   - encrypted SQLite (SQLCipher / platform keystore)
//   - hot backup and restore
//   - vacuum, compaction, and maintenance jobs
//   - multiple database profiles per device
//   - store.BlobStore with external blob references
//   - store.IDRegistryStore
//   - analytics, audit, and job queue stores
//   - materialized projection store
//
// # Atomic local write path
//
// High-level helper:
//
//	result, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
//	    Resource:      envelope,
//	    Action:        store.VersionActionCreate,
//	    SearchEntries: []store.SearchIndexEntry{…},
//	    Event:         event,
//	    Version:       version,
//	})
//
// Manual session (same transaction boundary):
//
//	session, err := db.BeginWrite(ctx)
//	session.ResourceStore().Create(ctx, envelope)
//	session.HistoryStore().AppendVersion(ctx, version)
//	session.EventStore().Append(ctx, event)
//	session.SearchStore().Index(ctx, entry)
//	session.Commit(ctx)
//
// ApplyLocalWrite and Session cover create, update, and delete. Delete removes current
// resource state, appends a history tombstone, emits an outbox event, and clears search
// index rows for the resource.
//
// # Search index field keys
//
// SearchStore routes fields to typed tables using key prefixes:
//
//   - token.<name>     → search_token
//   - string.<name>    → search_string
//   - date.<name>      → search_date
//   - number.<name>    → search_number
//   - reference.<name> or ref.<name> → search_reference
//
// Keys without a prefix (for example "family") default to search_string. FHIR search
// parsing and token extraction remain in pkg/search; this package stores prepared entries.
//
// QueryPrepared supports the "by-field" plan (args: key, value) matching pkg/store test doubles.
//
// # Schema and migrations
//
// Migrations are embedded numbered SQL files under migrations/ (for example 0001_init.sql).
// Migrate runs them idempotently and records applied versions in schema_migrations.
//
// Main tables:
//
//   - resource            — current canonical state keyed by (resource_type, id)
//   - resource_history    — immutable version log with delete tombstones
//   - search_*            — five typed index tables (no generic fallback)
//   - sync_outbox         — append-only local change events (AUTOINCREMENT sequence)
//   - sync_inbox_applied  — remote operation idempotency (scaffold)
//   - sync_cursor         — named consumer checkpoints
//   - sync_conflict       — conflict records (scaffold)
//   - binary_object       — inline binary metadata and payload (scaffold)
//   - module_registry     — local module registration metadata (scaffold)
//
// # Integration with other packages
//
// pkg/store   — interface contracts; sqlite implements the embedded-local subset.
// pkg/types   — ResourceEnvelope is the only FHIR container crossing store boundaries.
// pkg/postgres — tenant-scoped edge/cloud adapter with broader contract surface.
// pkg/core    — orchestrates write pipelines and version policy on top of store interfaces.
// pkg/search  — produces SearchIndexEntry values; does not implement SearchStore.
//
// # File layout
//
//   - doc.go              — package documentation (this file)
//   - db.go               — DB, Open, Migrate, ApplyLocalWrite, store constructors
//   - options.go          — Open options (WithBusyTimeout)
//   - migrate.go          — embedded migration runner
//   - session.go          — Session, BeginSession, atomic write pipeline
//   - resource_store.go   — ResourceStore
//   - history_store.go    — HistoryStore
//   - search_store.go     — SearchStore
//   - outbox_store.go     — OutboxStore (EventStore)
//   - inbox_store.go      — InboxStore (scaffold)
//   - cursor_store.go     — CursorStore
//   - conflict_store.go   — ConflictStore (scaffold)
//   - binary_store.go     — BinaryStore (scaffold)
//   - module_store.go     — ModuleStore (scaffold)
//   - timeutil.go         — RFC3339Nano timestamp helpers
//   - migrations/*.sql    — embedded schema migrations
//
// # Testing
//
// sqlite_test.go contains integration tests against temporary and :memory: databases:
// migration idempotency, per-store CRUD, ApplyLocalWrite atomicity and rollback, delete
// tombstones, concurrent smoke tests, and compose-style lifecycle tests mirroring
// pkg/store/store_test.go.
package sqlite
