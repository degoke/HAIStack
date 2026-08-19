# haistack-sync (`pkg/sync`)

Offline-first replication library for health-ai-stack — device-to-hub push/pull with canonical Postgres acceptance.

## What it does

**haistack-sync** is the **replication layer** between local SQLite nodes and a canonical Postgres hub. It follows a Git-inspired model:

| Branch | Role |
|--------|------|
| **Local SQLite node** | Provisional branch — offline writes append to the outbox |
| **Postgres hub** | Accepted branch — hub validates, accepts, and assigns canonical sequence/version |
| **Push** | Device proposes local events to the hub |
| **Pull** | Device replays accepted canonical events back locally |

Think of it as: *core writes a change locally → sync pushes it to the server → sync pulls canonical truth back → devices converge.*

```
pkg/core (write)  →  outbox (sqlite)  →  sync.Engine.Push  →  PostgresHub
                                                              ↓
pkg/core (read)   ←  local apply      ←  sync.Engine.Pull  ←  event_log (postgres)
```

The package has two layers:

1. **Outbox (write path)** — `Outbox`, `EventStoreOutbox`, `WithWriteSession` integrate with `pkg/core` so successful writes emit `store.ResourceEvent` inside the same transaction.
2. **Sync engine (replication)** — `Engine`, `Hub`, `PostgresHub` orchestrate push/pull, cursors, inbox idempotency, conflicts, jobs, and audit.

It does **not** parse FHIR on the device-side orchestration path or assign version IDs (that is
`pkg/core` + backends). `PostgresHub` validates pushed FHIR payloads before canonical acceptance.

It does **not**:
- Implement HTTP/MQTT transport (`Hub` is an in-process protocol boundary today)
- Resolve merge conflicts (`haistack-conflict` is planned; v1 detects and records only)
- Replace `pkg/store` persistence (it reuses existing contracts)

## When to use it

- **Offline-first apps** that write to SQLite and sync when online
- **Edge/cloud hubs** that accept device writes into Postgres `event_log`
- **Workers** that replay outbox events or process sync retry jobs
- **Tests** that need push/pull round-trips without a real network stack

Alias the import when you also use the standard library `sync` package:

```go
import hasync "github.com/degoke/health-ai-stack/pkg/sync"
```

## Usage

### 1. Enable outbox in core (local writes)

```go
import (
    hasync "github.com/degoke/health-ai-stack/pkg/sync"
    "github.com/degoke/health-ai-stack/pkg/core"
)

svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Resources: db.ResourceStore(),
    History:   db.HistoryStore(),
    Sessions:  db,
    Outbox:    &hasync.EventStoreOutbox{Events: db.OutboxStore()},
})
```

Core routes outbox appends through the active write session so resource, history, search, and outbox commit or roll back together.

### 2. Run sync on a device node

```go
hub := &hasync.PostgresHub{Tenant: tdb} // or a test double implementing hasync.Hub

engine := hasync.NewEngine(hasync.Config{
    NodeID:    "device-1",
    TenantID:  "tenant-a",
    Events:    sqliteDB.OutboxStore(),
    Cursors:   sqliteDB.CursorStore(),
	Inbox:     sqliteDB.InboxStore(),
	Resources: sqliteDB.ResourceStore(),
	History:   sqliteDB.HistoryStore(),
	Sessions:  sqliteDB,
	Conflicts: sqliteDB.ConflictStore(),
    Jobs:      jobStore,  // optional
    Audit:     auditStore, // optional
    Hub:       hub,
})

push, pull, err := engine.SyncOnce(ctx) // Push then Pull
```

Or run each pass separately:

```go
pushSummary, err := engine.Push(ctx)
pullSummary, err := engine.Pull(ctx)
```

### 3. Host a canonical hub (Postgres)

```go
hub := &hasync.PostgresHub{Tenant: tdb}

results, err := hub.Push(ctx, localEvents) // device-proposed events
canonical, err := hub.Pull(ctx, afterSequence, limit)
```

`PostgresHub` dedupes push by client `event_id`, checks base versions for stale writes, applies accepted writes via `postgres.ApplyWrite`, and returns per-event acknowledgements.

### 4. Optional: search on pull apply

If pulls should update the local search index atomically with resource/history/inbox changes, wire
both the database session provider and a `SearchIndexer` (for example from `pkg/search`):

```go
engine := hasync.NewEngine(hasync.Config{
    // …
    Sessions:      sqliteDB,
    Search:        sqliteDB.SearchStore(),
    SearchIndexer: indexer, // implements hasync.SearchIndexer
})
```

### 5. Background jobs

Enqueue and process retry/conflict/pull jobs with `store.JobStore`:

```go
processor := &hasync.JobProcessor{Engine: engine, Jobs: jobStore}
processed, err := processor.ProcessNext(ctx)
```

Job types: `sync.retry_push`, `sync.scheduled_pull`, `sync.conflict_processing`, `sync.event_replay`.

## Push and pull behaviour

**Push**

1. Read pending outbox events after the push cursor
2. Enrich into `LocalEvent` (stable `event_id`, base cloud version, payload)
3. Batch to hub in outbox sequence order
4. Handle ack per event: accepted, rejected, conflicted, already_processed, needs_retry
5. Record conflicts, audit, and retry jobs as needed
6. Advance push cursor only for terminal acks (not `needs_retry`)

**Pull**

1. Fetch canonical events after the pull cursor
2. Apply accepted events locally (idempotent via inbox)
3. Update resources/history without appending to outbox
4. Advance pull cursor after successful apply

**Idempotency keys**

| Direction | Key |
|-----------|-----|
| Push dedupe | `OutboxEventID(nodeID, tenantID, outboxSequence)` |
| Pull dedupe | `CanonicalEventID(tenantID, canonicalSequence)` |

## Config reference

| Field | Required? | Purpose |
|-------|-----------|---------|
| `NodeID`, `TenantID` | Yes | Device identity for protocol events and audit |
| `Events` | Yes (push) | Local outbox (`store.EventStore`) |
| `Hub` | Yes | Push/pull protocol adapter |
| `Resources`, `History` | Yes (pull) | Local apply target |
| `Sessions` | Recommended (pull) | Atomic resource/history/search/inbox apply |
| `Cursors` | No | Push/pull checkpoints (`sync.push`, `sync.pull`) |
| `Inbox` | No | Pull apply idempotency |
| `Conflicts`, `Jobs`, `Audit` | No | Side effects on push conflict/retry |
| `Search`, `SearchIndexer` | No | Index updates on pull apply |
| `PushBatchSize`, `PullBatchSize` | No | Default 100 |

## Mental model

**`pkg/sync` is “how local changes become canonical and how devices catch up.”**

- `pkg/core` emits minimal outbox signals on write
- `pkg/sync` enriches, transports (via `Hub`), acknowledges, and replays
- `pkg/postgres` stores canonical accepted events and hub inbox dedupe
- `pkg/sqlite` stores local outbox, inbox, and cursors on device

## What is deferred (v1)

- Network transport adapters (HTTP, MQTT)
- Peer-to-peer sync and encrypted/signed payloads
- Partial sync by patient/module/facility
- Rich merge policy and human conflict resolution
- `ChangedPaths` / `Patch` population on `LocalEvent` (fields exist, not filled yet)

## Where it fits

| Package | Role |
|---------|------|
| **core** | CRUD + optional outbox emission |
| **store** | EventStore, CursorStore, InboxStore, ConflictStore, JobStore, AuditStore |
| **sqlite** | Device outbox, inbox, cursors, local apply target |
| **postgres** | Canonical `event_log`, hub inbox, `PostgresHub` backend |
| **sync** | Protocol models, engine, push/pull, scheduler hooks |

See [doc.go](./doc.go) for the full API, file layout, and ownership boundaries.
