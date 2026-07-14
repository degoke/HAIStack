// Package sync implements haistack-sync, the offline-first replication library for Health AI Stack.
//
// haistack-sync layers a device-to-edge protocol on top of the minimal store.ResourceEvent
// outbox emitted by pkg/core. Local SQLite nodes act as provisional branches; canonical
// Postgres hubs act as accepted branches. Push proposes local events; pull replays accepted
// canonical events back to the node.
//
// # Public concepts
//
//   - Engine — high-level entrypoint for Push, Pull, and SyncOnce
//   - Config — local stores, hub adapter, node id, tenant id, clock
//   - Hub — protocol boundary for push/pull against a canonical server
//   - LocalEvent — rich local sync protocol payload enriched from outbox events
//   - CanonicalEvent — hub-assigned replay payload for ordered pull/apply
//   - PushResult — per-event acknowledgement with canonical metadata
//   - PostgresHub — canonical hub implementation backed by pkg/postgres
//
// # Outbox (existing)
//
// Outbox, EventStoreOutbox, SessionOutbox, and WithWriteSession remain the write-path
// emission boundary used by pkg/core. See outbox.go and session_outbox.go.
//
// # Store contracts
//
// The engine reuses pkg/store contracts directly:
//
//   - store.EventStore — local outbox on SQLite, canonical event_log on Postgres
//   - store.CursorStore — push and pull checkpoints
//   - store.InboxStore — pull/apply idempotency (and hub push dedupe)
//   - store.ConflictStore — stale-base and sync conflicts
//   - store.JobStore — retry and conflict follow-up jobs (processed via pkg/jobs)
//   - store.AuditStore — sync audit trail (actions via pkg/audit)
//
// # Typical device wiring
//
//	hub := &sync.PostgresHub{Tenant: tdb}
//	engine := sync.NewEngine(sync.Config{
//	    NodeID:    "device-1",
//	    TenantID:  "tenant-a",
//	    Events:    sqliteDB.OutboxStore(),
//	    Cursors:   sqliteDB.CursorStore(),
//	    Inbox:     sqliteDB.InboxStore(),
//	    Resources: sqliteDB.ResourceStore(),
//	    History:   sqliteDB.HistoryStore(),
//	    Hub:       hub,
//	})
//	_, _, err := engine.SyncOnce(ctx)
//
// # Ownership boundaries
//
// haistack-sync owns protocol models, outbox batching, inbox flow, push/pull orchestration,
// canonical acknowledgement handling, base-version checks, tombstone delete replication,
// cursor progression, retry scheduling hooks, and conflict/job/audit side effects.
// Detailed merge policy belongs in future haistack-conflict.
//
// # File layout
//
//   - doc.go              — package documentation
//   - outbox.go           — Outbox interface and EventStoreOutbox
//   - session_outbox.go   — transactional outbox routing for core
//   - event.go            — LocalEvent and CanonicalEvent models
//   - ack.go              — acknowledgement states and PushResult
//   - hub.go              — Hub interface, Config, cursor names
//   - enrich.go           — ResourceEvent → LocalEvent enrichment
//   - apply.go            — canonical pull apply pipeline
//   - push.go / pull.go   — push and pull orchestration
//   - engine.go           — Engine entrypoint
//   - server_postgres.go  — PostgresHub server
//   - jobs.go / audit.go  — job payloads and audit action names
//   - scheduler.go      — JobProcessor and scheduled pull enqueue
//   - search.go         — SearchIndexer hook for pull apply indexing
package sync
