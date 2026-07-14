# haistack-postgres (`pkg/postgres`)

Server-side PostgreSQL storage for health-ai-stack — the **authoritative backend** for multi-tenant cloud and edge deployments.

## What it does

This package stores FHIR resources and related data in **PostgreSQL** when you need a shared backend that many clients can talk to. It is the server counterpart to [`pkg/sqlite`](../sqlite), which is for a single device’s local offline database.

In simple terms, it is **the source of truth on the server**:

1. **Stores FHIR resources** — current state, version history, search indexes, binaries, audit logs, jobs, and more.
2. **Isolates tenants** — every operation is scoped to one tenant; tenant A never sees tenant B’s data through this API.
3. **Handles writes atomically** — a create, update, or delete either fully succeeds or fully rolls back (resource + history + events + search + audit together).
4. **Assigns server metadata** — on accepted writes it generates version IDs and global event sequence numbers; callers do not assign those.
5. **Records failures safely** — rejected or conflicted writes go to audit (and conflict tables) **without** changing the stored resource.

It does **not** parse FHIR, run FHIRPath, or implement HTTP APIs. It is pure persistence: open a database, then read and write through small interfaces defined in [`pkg/store`](../store).

## SQLite vs Postgres

| | **Postgres** (`pkg/postgres`) | **SQLite** (`pkg/sqlite`) |
|---|-------------------------------|---------------------------|
| **Role** | Shared server, many clients | One device, offline-first |
| **Tenancy** | Explicit per-tenant | Single local database |
| **Versions** | Server assigns version IDs | Caller supplies version IDs |
| **Events** | Global `event_log` sequence | Local `sync_outbox` |
| **When to use** | Cloud / edge service | Tablet, workstation, embedded node |

## Mental model

```
postgres.DB              ← one connection pool to Postgres
    └── Tenant("acme")   ← everything for one customer
            ├── ResourceStore()   read/write current Patient/Observation/…
            ├── HistoryStore()    past versions
            ├── EventStore()      ordered change stream (for sync/replay)
            ├── SearchStore()     index lookups
            ├── ApplyWrite()      one-shot “do the whole write pipeline”
            └── … audit, jobs, blobs, node registry, etc.
```

## When to use it

- Running a **cloud or edge server** that many devices sync with
- Need **tenant isolation** (multi-customer SaaS, regional tenants)
- Server must be **authoritative** for accepted resource versions and event ordering
- Background **jobs**, **audit**, **analytics**, or **materialized views** on the server

Use [`pkg/sqlite`](../sqlite) instead when the database lives on one device and syncs up later.

## Usage

### Connect and migrate

```go
import "github.com/degoke/health-ai-stack/pkg/postgres"

db, err := postgres.Open(ctx, "postgres://user:pass@localhost:5432/haistack?sslmode=disable")
if err != nil { /* handle */ }
defer db.Close()

if err := db.Migrate(ctx); err != nil { /* creates tables */ }
```

### Pick a tenant and read a resource

```go
tdb := db.Tenant("tenant-acme")

patient, err := tdb.ResourceStore().Read(ctx, "Patient", "pat-123")
```

### Accept a write (most common path)

`ApplyWrite` is the main entry point for creating, updating, or deleting a resource on the server:

```go
result, err := tdb.ApplyWrite(ctx, postgres.Write{
    Resource: envelope,                   // *types.ResourceEnvelope with JSON
    Action:   store.VersionActionCreate, // create | update | delete
    SearchEntries: []store.SearchIndexEntry{{
        ResourceType: "Patient",
        ID:           "pat-123",
        Fields:       map[string]string{"string.family": "Smith"},
    }},
    Audit: store.AuditRecord{Actor: "api", Action: "create"},
})
// result.Resource.VersionID — server-assigned version
// result.Event.Sequence     — global event number
```

On success, Postgres updates the resource, history, event log, search index, ID registry, and audit log in **one transaction**.

### Record a rejection or conflict (without changing data)

```go
_, err := tdb.ApplyWrite(ctx, postgres.Write{
    Resource:        envelope,
    Action:          store.VersionActionUpdate,
    Outcome:         postgres.WriteOutcomeConflicted,
    RejectionReason: "version mismatch",
    ConflictLocalVersionID:  "v-local",
    ConflictRemoteVersionID: "v-remote",
    Audit: store.AuditRecord{Actor: "sync", Action: "update"},
})
```

The stored resource **does not change**; you get audit and conflict records instead.

### Other stores

All hang off the same `TenantDB`:

| Accessor | Purpose |
|----------|---------|
| `EventStore()` | Replay changes (`ReadSince`) for sync workers |
| `CursorStore()` | Named checkpoints (“how far did this worker get?”) |
| `ConflictStore()` | List and resolve sync conflicts |
| `IDRegistry()` | Authoritative resource ID registration |
| `JobStore()` | Background job enqueue / claim / update |
| `AuditStore()` | Compliance and security audit trail |
| `NodeRegistry()` | Register edge devices and cloud nodes |
| `BlobStore()` / `BinaryStore()` | Inline or referenced binary payloads |
| `ModuleStore()` | Runtime module registration metadata |
| `MaterializedViewStore()` | Named read-optimized projections |
| `ReportingTableStore()` | Analytics reporting table snapshots for view refreshes |
| `AnalyticsStore()` | Append-only analytics events |

For manual multi-step writes in one transaction, use `BeginWrite` / `Session` and commit when done.

## Where it fits

| Layer | Role |
|-------|------|
| **types** | `ResourceEnvelope` — JSON container passed into stores |
| **store** | Interface contracts this package implements |
| **sqlite** | Local device copy; syncs accepted writes toward postgres |
| **postgres** | Tenant-scoped server persistence and authoritative writes |
| **core** | Orchestrates validation, version policy, and write pipelines upstream |

## Search index field keys

Search entries use typed table routing (same convention as sqlite):

- `token.<name>` → token index
- `string.<name>` → string index (default when no prefix)
- `date.<name>`, `number.<name>`, `reference.<name>` / `ref.<name>` → typed tables

FHIR search parsing lives outside this package; callers pass prepared `SearchIndexEntry` values on writes.

## Testing

Integration tests use [testcontainers](https://golang.testcontainers.org/) (`postgres:16-alpine`) or an external instance via:

```bash
TEST_POSTGRES_DSN='postgres://user:pass@localhost:5432/haistack?sslmode=disable' go test ./pkg/postgres/...
```

Without Docker or `TEST_POSTGRES_DSN`, integration tests skip.

## MVP limits

- Single Postgres database per `DB` (no built-in sharding or read-replica routing)
- Blob `Location` stores an opaque string; no object-storage SDK integration
- Partitioning, archival, and warehouse export cursors are future work
- FHIR validation, HTTP, and business rules live in upstream packages

See [doc.go](./doc.go) for design principles, schema tables, file layout, and the full write coordinator behavior.
