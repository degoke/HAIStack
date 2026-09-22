# haistack-sync (`pkg/sync`)

Offline-first replication library for haistack — device-to-hub push/pull with canonical Postgres acceptance.

## What it does

**haistack-sync** is the **replication layer** between local SQLite nodes and a canonical Postgres hub. It follows a Git-inspired model:

| Branch | Role |
|--------|------|
| **Local SQLite node** | Provisional branch — offline writes append to the outbox |
| **Postgres hub** | Accepted branch — hub validates, accepts, and assigns canonical sequence/version |
| **Push** | Device proposes local events to the hub |
| **Pull** | Device replays accepted canonical events back locally |

Think of it as: *core writes a change locally → sync pushes it to the server → sync pulls canonical truth back → devices converge.*

The package has two layers:

1. **Outbox (write path)** — `Outbox`, `EventStoreOutbox`, `SessionOutbox`, `WithWriteSession` integrate with `pkg/core` so successful writes emit `store.ResourceEvent` inside the same transaction.
2. **Sync engine (replication)** — `Engine`, `Hub`, `PostgresHub` orchestrate push/pull, cursors, inbox idempotency, conflicts, jobs, audit, and optional search reindex on pull apply.

Public protocol types include `LocalEvent`, `CanonicalEvent`, `PushResult`, acknowledgement states (`AckState`), and job payloads (`ConflictJobPayload`, `ReplayJobPayload`).

It does **not**:

- Parse FHIR on the device-side orchestration path or assign version IDs (`pkg/core` + backends)
- Implement mandatory HTTP/MQTT transport (`Hub` is an in-process protocol boundary; `pkg/http` exposes optional `/sync/push` and `/sync/pull`)
- Own merge policy (`pkg/conflict` evaluates conflicts; sync detects, persists, and enqueues)
- Replace `pkg/store` persistence (it reuses existing contracts)

`PostgresHub` validates pushed FHIR payloads before canonical acceptance.

## How it fits in the ecosystem

```
 pkg/core (ResourceService + optional Outbox)
        |
        v
 store.EventStore (sqlite outbox / postgres event_log)
        |
        v
 sync.Engine.Push  ----Hub---->  sync.PostgresHub  -->  postgres.ApplyWrite
        ^                              |
        |                              v
 sync.Engine.Pull  <----Hub----  canonical event_log + hub inbox dedupe
        |
        v
 local ResourceStore / HistoryStore / InboxStore / SearchStore (optional)
        |
        +--> store.ConflictStore + jobs (sync.conflict_processing)
        +--> pkg/conflict.Engine (via ConflictEngine config default)
        +--> store.AuditStore (sync audit actions)
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **core** | Emits minimal `ResourceEvent` rows when `Outbox` is wired |
| Upstream | **store** | Event, cursor, inbox, conflict, job, audit, resource, history contracts |
| Upstream | **conflict** | Default `ConflictEngine`; merge/review artifacts for job handler |
| Sidecar | **postgres** | `PostgresHub`, canonical `event_log`, hub inbox |
| Sidecar | **sqlite** | Device outbox, inbox, cursors |
| Downstream | **http** | Optional sync routes via `ScopedHubServer` |
| Downstream | **search** | Optional `SearchIndexer` on pull apply |
| Downstream | **jobs** | Retry push, scheduled pull, conflict processing, event replay |

## When to use it

- **Offline-first apps** that write to SQLite and sync when online
- **Edge/cloud hubs** that accept device writes into Postgres `event_log`
- **Workers** that replay outbox events or process sync retry jobs
- **Tests** that need push/pull round-trips without a real network stack
- **HTTP gateways** that expose hub push/pull to authenticated devices

Alias the import when you also use the standard library `sync` package:

```go
import hasync "github.com/degoke/haistack/pkg/sync"
```

## Usage modes

### 1. Enable outbox in core (local writes)

```go
import (
    hasync "github.com/degoke/haistack/pkg/sync"
    "github.com/degoke/haistack/pkg/core"
)

svc, err := core.NewResourceService(core.ResourceServiceConfig{
    Resources: db.ResourceStore(),
    History:   db.HistoryStore(),
    Sessions:  db,
    Outbox:    &hasync.EventStoreOutbox{Events: db.OutboxStore()},
})
```

Core routes outbox appends through the active write session so resource, history, search, and outbox commit or roll back together. Use `hasync.WithWriteSession` / `SessionOutbox` when binding outbox to an explicit session.

### 2. Device node: push, pull, or both

```go
hub := &hasync.PostgresHub{Tenant: tdb}

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
    Jobs:      jobStore,
    Audit:     auditStore,
    Hub:       hub,
})

push, pull, err := engine.SyncOnce(ctx)
// or separately:
pushSummary, err := engine.Push(ctx)
pullSummary, err := engine.Pull(ctx)
```

Cursor names default to `sync.push` and `sync.pull` (`CursorPush`, `CursorPull`).

### 3. Canonical hub (Postgres) without device engine

```go
hub := &hasync.PostgresHub{Tenant: tdb}

results, err := hub.Push(ctx, localEvents)
canonical, err := hub.Pull(ctx, afterSequence, limit)
```

`PostgresHub` dedupes push by client `event_id`, checks base versions for stale writes, applies accepted writes via `postgres.ApplyWrite`, and returns per-event acknowledgements.

### 4. Pull apply with search index updates

```go
engine := hasync.NewEngine(hasync.Config{
    // …
    Sessions:      sqliteDB,
    Search:        sqliteDB.SearchStore(),
    SearchIndexer: indexer, // implements hasync.SearchIndexer
})
```

When both `Sessions` and `SearchIndexer` are set, pull apply can persist resource, history, inbox, and search rows atomically.

### 5. Conflict jobs and custom resolution handler

```go
engine := hasync.NewEngine(hasync.Config{
    NodeID:                    "node-a",
    TenantID:                  "tenant-a",
    ConflictEngine:            conflict.NewDefaultEngine(),
    ConflictResolutionHandler: myHandler,
    // … stores and Hub …
})

processor := &hasync.JobProcessor{Engine: engine, Jobs: jobStore}
processed, err := processor.ProcessNext(ctx)
```

Job types (from `pkg/jobs`, re-exported in `sync`):

| Constant | Purpose |
|----------|---------|
| `JobTypeRetryPush` | Retry a failed push batch |
| `JobTypeScheduledPull` | Background pull |
| `JobTypeConflictProcessing` | Run `pkg/conflict` on a persisted conflict |
| `JobTypeEventReplay` | Replay push or pull from a sequence |

### 6. HTTP-scoped hub server

Implement `ScopedHubServer` when node and tenant identity come from the HTTP request:

```go
type tenantHub struct{ Inner hasync.HubServer }

func (h tenantHub) PushFor(ctx context.Context, nodeID, tenantID string, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
    return h.Inner.Push(ctx, events)
}
```

Wire through `pkg/http` `NewRootHandlerWithSyncMiddleware` for `/sync/push` and `/sync/pull`.

### 7. Test doubles implementing `Hub`

In-process tests can implement `hasync.Hub` with memory stores (see `integration_test.go`) without Postgres.

## Examples

**Stable event IDs for idempotency:**

```go
pushID := hasync.OutboxEventID(nodeID, tenantID, outboxSequence)
pullID := hasync.CanonicalEventID(tenantID, canonicalSequence)
```

**Inspect push acknowledgement:**

```go
for _, ack := range pushSummary.Results {
    switch ack.State {
    case hasync.AckAccepted, hasync.AckAlreadyProcessed:
        // terminal — cursor may advance
    case hasync.AckNeedsRetry:
        // cursor does not advance; retry job may enqueue
    case hasync.AckConflicted:
        // conflict record + conflict_processing job
    }
}
```

**Enqueue scheduled pull:**

```go
err := hasync.EnqueueScheduledPull(ctx, jobStore, nodeID, tenantID, time.Now().UTC().Add(time.Minute))
```

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

## Configuration reference

| Field | Required? | Purpose |
|-------|-----------|---------|
| `NodeID`, `TenantID` | Yes | Device identity for protocol events and audit |
| `Events` | Yes (push) | Local outbox (`store.EventStore`) |
| `Hub` | Yes | Push/pull protocol adapter |
| `Resources`, `History` | Yes (pull) | Local apply target |
| `Sessions` | Recommended (pull) | Atomic resource/history/search/inbox apply |
| `Cursors` | No | Push/pull checkpoints |
| `Inbox` | No | Pull apply idempotency |
| `Conflicts`, `Jobs`, `Audit` | No | Side effects on push conflict/retry |
| `Search`, `SearchIndexer` | No | Index updates on pull apply |
| `ConflictEngine` | No | Defaults to `conflict.NewDefaultEngine()` |
| `ConflictResolutionHandler` | No | Replay/resubmit or surface review UI |
| `PushBatchSize`, `PullBatchSize` | No | Default 100 |

## Where it fits

| Package | Role |
|---------|------|
| **core** | CRUD + optional outbox emission |
| **store** | EventStore, CursorStore, InboxStore, ConflictStore, JobStore, AuditStore |
| **sqlite** | Device outbox, inbox, cursors, local apply target |
| **postgres** | Canonical `event_log`, hub inbox, `PostgresHub` backend |
| **conflict** | Merge policy invoked from conflict jobs |
| **search** | Optional pull-time indexing |
| **http** | Optional REST sync endpoints |
| **sync** | Protocol models, engine, push/pull, scheduler hooks |

## Limits

- Network transport adapters beyond in-process `Hub` and optional HTTP routes are application-owned
- Peer-to-peer sync and encrypted/signed payloads are deferred
- Partial sync by patient/module/facility is not implemented
- Rich merge policy defaults to human review except safe-list paths in `pkg/conflict`
- `ChangedPaths` / `Patch` on `LocalEvent` exist but are not populated by enrich yet
- Push cursor does not advance on `needs_retry` — callers must retry or run jobs

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — offline-first replication overview
- [pkg/core/README.md](../core/README.md) — resource writes and outbox hook
- [pkg/conflict/README.md](../conflict/README.md) — merge and review artifacts
- [pkg/store/README.md](../store/README.md) — event, cursor, inbox stores
- [pkg/postgres/README.md](../postgres/README.md) — hub backend and `event_log`
- [pkg/sqlite/README.md](../sqlite/README.md) — device-side stores
- [pkg/http/README.md](../http/README.md) — `/sync/push` and `/sync/pull` routes
- [pkg/jobs/README.md](../jobs/README.md) — job runner for sync job types
- [doc.go](./doc.go) — full API, file layout, and ownership boundaries
