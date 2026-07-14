// Package postgres implements haistack-postgres, the authoritative edge/cloud storage
// backend for health-ai-stack.
//
// haistack-postgres is the Postgres-backed implementation of pkg/store contracts for
// tenant-scoped server deployments. Unlike haistack-sqlite (pkg/sqlite), it persists
// accepted writes as the system of record, assigns version IDs and global event sequence
// numbers inside the adapter, enforces tenant boundaries, and records rejected or
// conflicted writes without mutating current resource state. This is not bytefhir-postgres;
// it is the haistack runtime's server persistence adapter at
// github.com/degoke/health-ai-stack/pkg/postgres.
//
// # Design principles
//
// Tenant-scoped access:
//
//   - DB holds a shared pgxpool.Pool; TenantDB scopes every query with a tenant_id.
//   - Callers obtain stores through db.Tenant(tenantID); cross-tenant reads and writes
//     are not exposed on the public API.
//   - EnsureTenant (on DB or implicitly via BeginSession/ApplyWrite) registers the tenant
//     row in the tenant table before writes proceed.
//
// Authoritative accepted writes:
//
//   - ApplyWrite and Session coordinate one Postgres transaction for each write attempt.
//   - Accepted writes assign a new VersionID (UUID) and append to event_log with a
//     globally monotonic BIGSERIAL sequence assigned inside the same transaction.
//   - Accepted writes update resource, resource_history, event_log, typed search_* tables,
//     resource_id_registry, and audit_log atomically.
//   - Rejected or conflicted writes append audit_log only; conflicted writes also append
//     sync_conflict. They do not change resource, resource_history, event_log, or search rows.
//
// Canonical JSON:
//
//   - types.ResourceEnvelope.JSON is the source of truth for resource content.
//   - Stores persist JSONB plus metadata columns (version_id, last_updated, hash).
//   - Search extraction and FHIR parsing remain outside this package; callers supply
//     prepared store.SearchIndexEntry values on accepted writes.
//
// pgx driver:
//
//   - Uses github.com/jackc/pgx/v5 and pgxpool directly (not database/sql).
//   - Open accepts functional options such as WithMaxConns and WithMinConns.
//
// # Opening a database
//
//	db, err := postgres.Open(ctx, "postgres://user:pass@localhost:5432/haistack?sslmode=disable")
//	if err != nil { … }
//	defer db.Close()
//
//	if err := db.Migrate(ctx); err != nil { … }
//
// Obtain a tenant-scoped accessor:
//
//	tdb := db.Tenant("tenant-a")
//	envelope, err := tdb.ResourceStore().Read(ctx, "Patient", "pat-1")
//
// # Implemented contracts
//
// All types below satisfy pkg/store interfaces unless noted. Every store is tenant-scoped
// through TenantDB:
//
//   - ResourceStore         → store.ResourceStore         — current-state CRUD (resource table)
//   - HistoryStore          → store.HistoryStore          — immutable history and tombstones
//   - SearchStore           → store.SearchStore           — typed search_* index tables
//   - EventStore            → store.EventStore            — append-only event_log with global sequence
//   - CursorStore           → store.CursorStore           — named sync_cursor checkpoints
//   - InboxStore            → store.InboxStore            — sync_inbox_applied push/pull dedupe
//   - ConflictStore         → store.ConflictStore         — sync_conflict append/list/resolve
//   - IDRegistry            → store.IDRegistryStore       — resource_id_registry reserve/check/register
//   - BinaryStore           → store.BinaryStore           — inline binary_object payloads
//   - BlobStore             → store.BlobStore             — binary_object with optional Location
//   - AuditStore            → store.AuditStore            — append-only audit_log
//   - ModuleStore           → store.ModuleStore           — module_registry metadata
//   - MaterializedViewStore → store.MaterializedViewStore — materialized_view projections
//   - ReportingTableStore   → store.ReportingTableStore   — analytics reporting snapshots
//   - AnalyticsStore        → store.AnalyticsStore        — analytics_event append/query
//   - JobStore              → store.JobStore              — background_job enqueue/claim/update
//   - NodeRegistry          → store.NodeRegistryStore     — node_registry for edge/cloud nodes
//   - Session               → store.WriteSession          — transaction-scoped core stores
//   - TenantDB              → store.WriteSessionProvider  — via BeginWrite / BeginSession
//
// Session also exposes IDRegistry, ConflictStore, and AuditStore for manual multi-step
// server writes inside one transaction; those methods are not part of store.WriteSession.
//
// # Atomic write path
//
// High-level helper for accepted, rejected, or conflicted writes:
//
//	result, err := tdb.ApplyWrite(ctx, postgres.Write{
//	    Resource:      envelope,
//	    Action:        store.VersionActionCreate,
//	    SearchEntries: []store.SearchIndexEntry{…},
//	    RequestedID:   "pat-1", // optional; overrides Resource.ID on create
//	    Audit:         store.AuditRecord{Actor: "api", Action: "create"},
//	    Outcome:       postgres.WriteOutcomeAccepted, // default when empty
//	})
//
// Rejected write (audit only; no resource mutation):
//
//	_, err := tdb.ApplyWrite(ctx, postgres.Write{
//	    Resource:        envelope,
//	    Action:          store.VersionActionUpdate,
//	    Outcome:         postgres.WriteOutcomeRejected,
//	    RejectionReason: "validation failed",
//	    Audit:           store.AuditRecord{…},
//	})
//
// Conflicted write (audit + sync_conflict; no resource mutation):
//
//	_, err := tdb.ApplyWrite(ctx, postgres.Write{
//	    Resource:                envelope,
//	    Action:                  store.VersionActionUpdate,
//	    Outcome:                 postgres.WriteOutcomeConflicted,
//	    RejectionReason:         "version mismatch",
//	    ConflictLocalVersionID:  localVersion,
//	    ConflictRemoteVersionID: remoteVersion,
//	    Audit:                   store.AuditRecord{…},
//	})
//
// Manual session (same transaction boundary for accepted writes composed by hand):
//
//	session, err := tdb.BeginWrite(ctx)
//	session.ResourceStore().Create(ctx, envelope)
//	session.HistoryStore().AppendVersion(ctx, version)
//	session.EventStore().Append(ctx, event)
//	session.SearchStore().Index(ctx, entry)
//	session.Commit(ctx)
//
// ApplyWrite on accepted outcomes assigns VersionID and event sequence; manual sessions
// require the caller to supply version metadata and events unless using lower-level stores
// directly without the write coordinator.
//
// WriteResult returns the persisted envelope (with assigned VersionID on acceptance),
// ResourceVersion, ResourceEvent (with Sequence), IDRegistryResult, and WriteOutcome.
//
// # Search index field keys
//
// SearchStore routes fields to typed tables using key prefixes (same convention as sqlite):
//
//   - token.<name>                     → search_token
//   - string.<name>                    → search_string
//   - date.<name>                      → search_date
//   - number.<name>                    → search_number
//   - reference.<name> or ref.<name>   → search_reference
//
// Keys without a prefix default to search_string. QueryPrepared supports the "by-field"
// plan (args: key, value). AnalyticsStore QueryPrepared supports "by-name-since"
// (args: name, since as RFC3339).
//
// # ID registry
//
// On accepted creates, ApplyWrite calls IDRegistry.Reserve to claim (tenant_id,
// resource_type, id) exclusively; duplicate creates fail the transaction. Updates and
// deletes verify or lazily register IDs. Conflict and rejection paths skip registry changes.
//
// # Schema and migrations
//
// Migrations are embedded numbered SQL files under migrations/ (for example 0001_init.sql).
// Migrate runs them idempotently and records applied versions in schema_migrations.
//
// Main tables (all tenant-aware where applicable):
//
//   - tenant                — authoritative tenant registry
//   - resource              — current accepted state keyed by (tenant_id, resource_type, id)
//   - resource_history      — immutable accepted version log with delete tombstones
//   - event_log             — globally ordered accepted-write events (BIGSERIAL sequence)
//   - resource_id_registry  — authoritative resource ID registration per type
//   - node_registry         — edge/cloud node metadata for sync coordination
//   - sync_conflict         — rejected/conflicted write records and resolution timestamps
//   - search_*              — five typed index tables (token, string, date, number, reference)
//   - sync_cursor           — named consumer checkpoints
//   - binary_object         — inline binary or blob metadata (optional location column)
//   - audit_log             — append-only audit entries
//   - module_registry       — tenant module registration metadata
//   - materialized_view     — named projection entries
//   - analytics_reporting_meta / analytics_reporting_row — analytics reporting table snapshots
//   - analytics_event       — append-only analytics records
//   - background_job        — durable job queue with claim semantics (FOR UPDATE SKIP LOCKED)
//
// # Integration with other packages
//
// pkg/store   — interface contracts; postgres implements the full server-side surface.
// pkg/types   — ResourceEnvelope is the only FHIR container crossing store boundaries.
// pkg/sqlite  — embedded local adapter; callers sync local outbox events to server event_log.
// pkg/core    — orchestrates write pipelines, validation, and conflict policy upstream.
// pkg/search  — produces SearchIndexEntry values; does not implement SearchStore.
//
// # File layout
//
//   - doc.go               — package documentation (this file)
//   - db.go                — DB, Open, Migrate, Tenant, EnsureTenant, Pool
//   - tenant.go            — TenantDB and per-store accessors, BeginWrite, ApplyWrite
//   - options.go           — Open options (WithMaxConns, WithMinConns)
//   - migrate.go           — embedded migration runner
//   - session.go           — Session, Write, WriteResult, WriteOutcome, write coordinator
//   - exec.go                — internal querier/execer abstractions for pool vs tx
//   - util.go                — null helpers and pgx.ErrNoRows check
//   - resource_store.go    — ResourceStore
//   - history_store.go     — HistoryStore
//   - search_store.go      — SearchStore
//   - event_store.go       — EventStore (event_log)
//   - cursor_store.go      — CursorStore
//   - conflict_store.go    — ConflictStore
//   - id_registry.go       — IDRegistry
//   - binary_store.go      — BinaryStore
//   - blob_store.go        — BlobStore
//   - audit_store.go       — AuditStore
//   - module_store.go      — ModuleStore
//   - node_registry.go     — NodeRegistry
//   - materialized_views.go — MaterializedViewStore
//   - reporting_table_store.go — ReportingTableStore
//   - analytics_store.go   — AnalyticsStore
//   - job_store.go         — JobStore
//   - migrations/*.sql     — embedded schema migrations
//
// # Testing
//
// postgres_test.go contains integration tests against a real Postgres instance via
// testcontainers (postgres:16-alpine) or TEST_POSTGRES_DSN when set. Tests cover migration
// idempotency, tenant isolation, per-store CRUD, ApplyWrite atomicity and rollback,
// accepted/rejected/conflicted write semantics, delete tombstones, ID registry races,
// conflict resolution, job claim/update, materialized views, analytics queries, node
// registry, and concurrent global event sequencing.
//
// When Docker is unavailable and TEST_POSTGRES_DSN is unset, integration tests skip.
// initDockerHost resolves DOCKER_HOST from the active docker context for OrbStack and
// similar environments where the default socket path differs.
//
// # Out of scope (later work)
//
// Partitioning by tenant or resource type, read replicas, hot/cold storage, archival history,
// object-storage integration for BlobStore.Location, warehouse export cursors, encrypted
// connections beyond DSN configuration, multi-database routing, and in-process caching layers.
package postgres
