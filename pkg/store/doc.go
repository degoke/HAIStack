// Package store defines haistack-store, the storage contract layer for the health-ai-stack
// runtime.
//
// haistack-store gives every downstream package one stable set of interfaces for resource
// persistence, history, indexing, events, transactions, binaries, cursors, conflicts, and
// operational storage without coupling the rest of the system to SQLite, Postgres, Redis,
// OpenSearch, or any other backend. Implementations live in separate packages (for example
// pkg/sqlite for embedded local storage and pkg/postgres for tenant-scoped edge/cloud storage).
//
// This package defines contracts and shared record types only:
//   - It depends on pkg/types for types.ResourceEnvelope and nothing else for FHIR shape.
//   - It does not own FHIR business rules, version assignment policy, search parsing,
//     FHIRPath, HTTP concerns, scheduling policy, or reconciliation logic.
//   - Canonical FHIR JSON stays in types.ResourceEnvelope.JSON; no alternate
//     canonicalization rules are defined here.
//
// # Design principles
//
// Current-state vs history:
//
//   - ResourceStore holds the latest version of each resource (CRUD on current state).
//   - HistoryStore holds immutable version history; delete is current-state removal plus a
//     historical tombstone (ResourceVersion with Action=delete and Deleted=true).
//   - VersionID assignment is caller-owned; stores persist what they are given.
//
// Index vs search:
//
//   - SearchStore is index-oriented persistence only (tokens, fields, prepared lookups).
//   - Raw FHIR search expressions are parsed outside this package (for example pkg/search).
//
// Events vs cursors:
//
//   - EventStore is append-only local change events for replay, sync, and projections.
//   - CursorStore persists consumer checkpoints independently so workers can resume safely.
//   - InboxStore records applied remote operation ids for idempotent sync replay.
//
// Transactions:
//
//   - Transactor exposes optional low-level BeginTx/Commit/Rollback for backends that support
//     SQL-style transactions.
//   - WriteSessionProvider coordinates a higher-level atomic write across ResourceStore,
//     HistoryStore, SearchStore, and EventStore in one session.
//   - Not every adapter must implement transactional scopes; in-memory or read-only backends
//     may omit them.
//
// Policy stays upstream:
//
//   - ConflictStore records conflicts; reconciliation rules live in pkg/core or sync layers.
//   - JobStore persists job state; retry, scheduling, and worker execution live elsewhere.
//   - ModuleStore registers module metadata; activation and code loading live elsewhere.
//   - AnalyticsStore and AuditStore append records; reporting and compliance workflows live
//     elsewhere.
//
// # Core resource contracts
//
// ResourceStore — current-state CRUD for FHIR resources:
//
//   - Create(ctx, res) inserts a new resource envelope.
//   - Read(ctx, resourceType, id) returns the latest envelope or an error when absent.
//   - Update(ctx, res) replaces the current envelope for the given type and id.
//   - Delete(ctx, resourceType, id) removes the current record (history tombstones use
//     HistoryStore separately).
//   - Exists(ctx, resourceType, id) reports whether a current record is present.
//
// HistoryStore — immutable lifecycle history:
//
//   - AppendVersion(ctx, version) appends one ResourceVersion entry.
//   - GetHistory(ctx, resourceType, id) returns ordered history for one resource.
//
// ResourceVersion fields:
//
//   - ResourceType, ID, VersionID — resource identity for this history entry.
//   - Action — VersionActionCreate, VersionActionUpdate, or VersionActionDelete.
//   - Timestamp — when the version was recorded.
//   - Resource — optional envelope snapshot (typically omitted for delete tombstones).
//   - Hash — content digest copied from the envelope or supplied by the caller.
//   - Deleted — true for delete tombstone entries (Action should be VersionActionDelete).
//
// SearchStore — search-index persistence:
//
//   - Index(ctx, entry) upserts SearchIndexEntry metadata or tokens for one resource.
//   - RemoveIndex(ctx, resourceType, id) drops all indexed data for that resource.
//   - Lookup(ctx, key, value) returns candidate resource IDs for a simple key/value pair.
//   - QueryPrepared(ctx, query, args) runs a backend-specific prepared lookup plan
//     (PreparedQuery.Name identifies the plan; args carry bound parameters).
//
// EventStore — append-only local change events:
//
//   - Append(ctx, event) stores a ResourceEvent and returns it with Sequence assigned when
//     the caller left Sequence unset.
//   - ReadSince(ctx, afterSequence, limit) reads events after a checkpoint for replay or sync.
//
// ResourceEvent fields mirror history at the change-notification layer: Sequence, ResourceType,
// ID, VersionID, Action (EventActionCreate/Update/Delete), Timestamp, and Hash.
//
// # Sync and coordination contracts
//
// CursorStore — named consumer checkpoints:
//
//   - GetCursor(ctx, name) reads one Cursor (Position may be empty for a fresh consumer).
//   - UpsertCursor(ctx, cursor) creates or replaces a checkpoint.
//   - DeleteCursor(ctx, name) removes a checkpoint.
//
// ConflictStore — first-class conflict records:
//
//   - Append(ctx, record) stores a ConflictRecord.
//   - List(ctx, resourceType, resourceID) returns conflicts for one resource.
//   - Resolve(ctx, id, resolvedAt) marks a conflict resolved without deleting history.
//
// IDRegistryStore — authoritative ID registration (typically tenant-scoped in adapters):
//
//   - Check(ctx, resourceType, id) reports whether an id is already registered.
//   - Reserve(ctx, resourceType, id) holds an id during an in-flight write.
//   - Register(ctx, resourceType, id) commits a successful registration.
//
// IDRegistryEntry and IDRegistryResult are supporting record types for registration outcomes;
// adapters may return them from higher-level write pipelines outside these interface methods.
//
// # Media contracts
//
// BinaryStore — small inline binary payloads keyed by stable string:
//
//   - Put(ctx, obj), Get(ctx, key), Delete(ctx, key).
//   - BinaryObject carries Key, ContentType, Size, Hash, Data, and CreatedAt.
//
// BlobStore — larger blobs or opaque backend references:
//
//   - Put(ctx, obj), Get(ctx, key), Head(ctx, key), Delete(ctx, key).
//   - BlobObject adds Location for an opaque backend locator without exposing object-storage
//     SDK types. Head returns metadata without payload bytes when the backend supports it.
//
// BinaryStore is for compact payloads stored inline; BlobStore is for larger content or
// externally referenced storage.
//
// # Projection and operations contracts
//
// MaterializedViewStore — persisted read-optimized projections:
//
//   - Upsert(ctx, record), Get(ctx, viewName, key), Delete(ctx, viewName, key),
//     ListKeys(ctx, viewName).
//   - MaterializedViewRecord holds ViewName, Key, Payload, Version, and UpdatedAt.
//   - Projection logic and refresh policy remain outside this package.
//
// AnalyticsStore — operational or product analytics events:
//
//   - Append(ctx, event) stores an AnalyticsEvent.
//   - QueryPrepared(ctx, query, args) reads events through a backend-specific prepared query.
//   - Does not compute aggregates or define dashboards.
//
// AuditStore — compliance and security audit trail:
//
//   - Append(ctx, record) stores an append-only AuditRecord.
//   - List(ctx, query) returns records filtered by AuditQuery (resource, actor, time range,
//     limit). Distinct from AnalyticsStore: audit entries capture actor, action, and outcome
//     for compliance review.
//
// JobStore — durable background job queue:
//
//   - Enqueue(ctx, job), ClaimNext(ctx, jobType), Update(ctx, job), Get(ctx, id).
//   - JobRecord tracks Type, Payload, Status (pending/running/completed/failed), Attempts,
//     CreatedAt, UpdatedAt, RunAfter, and LastError.
//   - Retry and scheduling policy remain outside this package.
//
// ModuleStore — runtime module registration metadata:
//
//   - Register(ctx, module), Get(ctx, name), List(ctx), Unregister(ctx, name).
//   - ModuleRecord holds Name, Version, Metadata, and RegisteredAt.
//   - Does not load code or enforce which modules are active.
//
// NodeRegistryStore — edge and cloud node registration for sync coordination:
//
//   - Register(ctx, node), Get(ctx, nodeID), List(ctx), Unregister(ctx, nodeID).
//   - NodeRecord holds NodeID, Metadata, and RegisteredAt.
//   - Does not perform heartbeat, lease, or leader election.
//
// # Transaction boundaries
//
// Transaction and Transactor — optional low-level transactions:
//
//   - BeginTx(ctx) returns a Transaction with Commit() and Rollback().
//   - When supported, all writes through the transaction-scoped stores must commit atomically
//     according to the backend's rules.
//
// WriteSession and WriteSessionProvider — coordinated atomic writes:
//
//   - BeginWrite(ctx) returns a WriteSession exposing ResourceStore(), HistoryStore(),
//     SearchStore(), and EventStore() bound to one unit of work.
//   - Commit(ctx) or Rollback(ctx) finishes the session.
//   - Adapters such as pkg/sqlite.Session and pkg/postgres write pipelines implement this
//     pattern for local and tenant-scoped atomic resource writes.
//
// # Typical flows
//
// Simple resource read:
//
//	envelope, err := resourceStore.Read(ctx, "Patient", "pat-1")
//
// Full local write path (composed by pkg/core or an adapter session):
//
//  1. resourceStore.Create or Update with types.ResourceEnvelope
//  2. historyStore.AppendVersion with matching ResourceVersion
//  3. eventStore.Append with matching ResourceEvent
//  4. searchStore.Index with extracted SearchIndexEntry fields
//  5. On delete: resourceStore.Delete, history tombstone, delete event, searchStore.RemoveIndex
//
// Sync replay:
//
//	events, err := eventStore.ReadSince(ctx, cursorPosition, limit)
//	… process events …
//	cursorStore.UpsertCursor(ctx, store.Cursor{Name: worker, Position: lastSequence})
//
// Conflict capture (policy elsewhere):
//
//	conflictStore.Append(ctx, store.ConflictRecord{…})
//	… later …
//	conflictStore.Resolve(ctx, id, time.Now())
//
// Atomic write session:
//
//	session, err := provider.BeginWrite(ctx)
//	session.ResourceStore().Create(ctx, envelope)
//	session.HistoryStore().AppendVersion(ctx, version)
//	session.EventStore().Append(ctx, event)
//	session.SearchStore().Index(ctx, entry)
//	session.Commit(ctx)
//
// # Integration with other packages
//
// pkg/types — ResourceEnvelope is the only FHIR resource container crossing store boundaries.
// Hash, VersionID, and LastUpdated on envelopes are derived in pkg/types; stores persist them
// as supplied.
//
// pkg/sqlite — embedded local implementation of core contracts (ResourceStore, HistoryStore,
// SearchStore, EventStore via outbox, CursorStore, ConflictStore, BinaryStore) plus
// transaction-scoped Session for atomic offline writes.
//
// pkg/postgres — tenant-scoped edge/cloud implementation covering the broader contract surface
// (including BlobStore, MaterializedViewStore, AnalyticsStore, JobStore, AuditStore,
// ModuleStore, NodeRegistryStore, IDRegistryStore, and WriteSession-style pipelines).
//
// pkg/core — orchestrates business rules, version assignment, and write pipelines on top of
// store interfaces without importing backend packages directly in hot paths where possible.
//
// pkg/search — parses FHIR search parameters and produces index entries or queries; it does
// not implement SearchStore.
//
// In-memory test doubles and future pkg/store/memory adapters implement subsets of these
// interfaces for tests and lightweight deployments.
//
// # File layout
//
//   - doc.go — package documentation (this file).
//   - resource.go — ResourceStore.
//   - history.go — HistoryStore, ResourceVersion, VersionAction.
//   - search.go — SearchStore, SearchIndexEntry, PreparedQuery.
//   - events.go — EventStore, ResourceEvent, EventAction.
//   - transaction.go — Transaction, Transactor.
//   - write_session.go — WriteSession, WriteSessionProvider.
//   - id_registry.go — IDRegistryStore, IDRegistryEntry, IDRegistryResult.
//   - binary.go — BinaryStore, BinaryObject.
//   - blob.go — BlobStore, BlobObject.
//   - cursor.go — CursorStore, Cursor.
//   - inbox.go — InboxStore for sync idempotency.
//   - conflict.go — ConflictStore, ConflictRecord.
//   - materialized_view.go — MaterializedViewStore, MaterializedViewRecord.
//   - analytics.go — AnalyticsStore, AnalyticsEvent.
//   - audit.go — AuditStore, AuditRecord, AuditQuery.
//   - job.go — JobStore, JobRecord, JobStatus.
//   - module.go — ModuleStore, ModuleRecord.
//   - node_registry.go — NodeRegistryStore, NodeRecord.
//
// # Testing
//
// store_test.go contains contract-focused tests: compile-time interface satisfaction checks
// using in-memory implementations, JSON round-trip tests for record types, tombstone history
// representation, cursor empty/non-empty positions, transaction commit/rollback, composed
// write paths, and behavioral tests for each store interface. Backend-specific correctness
// belongs in pkg/sqlite and pkg/postgres tests.
//
// # Out of scope (MVP)
//
// Concrete database adapters (except as separate packages), in-memory production adapters
// (pkg/store/memory is future work), tenant models embedded in every interface, bulk
// import/export APIs, streaming query interfaces, object-storage SDK types, FHIR search
// parsing, version assignment policy, conflict reconciliation, job execution engines,
// analytics aggregation, and audit retention workflows.
package store
