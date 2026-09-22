# haistack-jobs (`pkg/jobs`)

Shared background job runtime for HAIStack.

## What it does

**haistack-jobs** is the **shared worker runtime** for asynchronous work across the monorepo. It sits on top of `store.JobStore` — the persistence contract — and adds handler registration, claim/dispatch loops, retry/backoff, typed enqueue helpers, payload marshaling, job ownership stamping, durable **status row** helpers, and an in-memory store for tests.

Job **type strings are opaque** to this package: only types you register on `Runner` are executed. Conventions favor dotted names scoped by owning package (`sync.retry_push`, `search.reindex`). Legacy short names such as `reindex` remain valid for compatibility.

**Claim semantics** (implemented by `InMemoryJobStore`, `pkg/sqlite`, and `pkg/postgres`) select the oldest **pending** job of the requested type whose `RunAfter` is zero or not after now, transition status to **running**, and increment **Attempts** atomically with the claim. `Runner` then marks the job completed, reschedules pending with backoff, or fails terminally via `ApplyHandlerResult`.

The package also defines **well-known job types** for search, sync, registry/terminology, modules, views, analytics, exports, and subscriptions — plus helpers like `EnqueuePackPreExpand` used by `pkg/registry` after package install.

## What it does not do

- **Define SQL schemas or open databases** — use `pkg/sqlite` or `pkg/postgres` for durable `JobStore`
- **Implement domain work** — search reindex logic lives in `pkg/search`; sync retry in `pkg/sync`; terminology workers in this package only wrap registry/terminology job types
- **Provide distributed locking across nodes** — correctness relies on store-level atomic `ClaimNext`; multi-worker deployments need a store that serializes claims
- **Schedule cron or delayed enqueue beyond `RunAfter`** — callers set `EnqueueOptions.RunAfter` explicitly

Jobs owns **how work is claimed and retried**, not **what** the work does.

## How it fits in the ecosystem

```
  Application / pkg/search / pkg/sync / pkg/registry
                        |
                        |  Enqueue(ctx, JobStore, type, payload)
                        v
                 store.JobStore  <--- sqlite.DB / postgres.TenantDB
                        ^
                        |  ClaimNext(type) + Update(status)
                        |
                   jobs.Runner  ---> Handler.HandleJob(job)
                        |
            ApplyHandlerResult (retry / complete / fail)
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **store** | `JobStore`, optional `JobCASStore`, `JobDeleter` |
| Upstream | **sqlite** / **postgres** | Durable job persistence |
| Peer | **search** | Registers handlers for `search.reindex` / `reindex` |
| Peer | **sync** | Registers handlers for `sync.*` job types |
| Peer | **registry** | Calls `EnqueuePackPreExpand`; workers for terminology install/pre-expand |
| Peer | **modules** | `modules.install` payload and type constants |
| Downstream | **export** / **analytics** / **view** | Status rows via `StatusStore` and export job type constants |

Typical flow: a feature package enqueues through `jobs.Enqueue`, a long-running process calls `Runner.RunLoop`, and handlers mutate other stores or update typed status rows with `StatusStore`.

## When to use it

- Running background workers in a server process (`Runner.RunLoop` or periodic `RunOnce`)
- Unit and integration tests that need claim/retry semantics without Postgres (`NewInMemoryJobStore`)
- Enqueueing work with stable IDs, tenant/principal ownership, or delayed retry (`EnqueueOptions`, `StampOwner`)
- Persisting FHIR bulk export/import **status** separately from executable work rows (`TypeExportBulkRecord`, `StatusStore`)
- Registry-driven ValueSet pre-expand after package install (`EnqueuePackPreExpand`)
- Normalizing missing-job errors across backends (`IsMissing`, `GetRecord`, `Lookup`)

## Usage modes

### In-memory runner for tests and local dev

Register handlers, enqueue jobs, and process one or many iterations:

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/jobs"
    "github.com/degoke/haistack/pkg/store"
)

ctx := context.Background()
jobStore := jobs.NewInMemoryJobStore()

runner := jobs.NewRunner(jobStore)
runner.MaxAttempts = 5
runner.Backoff = jobs.DefaultBackoff()

_ = runner.Register(jobs.TypeReindex, jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
    // domain logic
    return nil
}))

_, _ = jobs.Enqueue(ctx, jobStore, jobs.TypeReindex, map[string]string{"resourceType": "Patient"}, jobs.EnqueueOptions{
    ID: "reindex-patient-1",
})

processed, err := runner.RunOnce(ctx)
_ = processed
_ = err
// go runner.RunLoop(ctx) for a long-lived worker
```

### Durable store via SQLite or Postgres

Persistence adapters implement the same `store.JobStore` interface; the runner code is identical:

```go
jobStore := db.JobStore() // sqlite.DB or postgres.TenantDB
runner := jobs.NewRunner(jobStore)
```

Use `Enqueue` / `NewJob` the same way; only the store constructor changes.

### Typed status rows (bulk export and similar)

Keep a **non-claimable** job type for durable status while workers use a separate executable type:

```go
status := jobs.NewStatusStore[BulkExportStatus](jobStore, jobs.TypeExportBulkRecord)
err := status.Create(ctx, exportID, BulkExportStatus{ /* ... */ }, store.JobStatusRunning, "", time.Now().UTC())
rec, err := status.Get(ctx, exportID)
_ = rec
```

`TypeExportBulkRecord` and `TypeImportBulkRecord` are intentionally distinct from `TypeExportBulk` / `TypeImportBulk` so `ClaimNext` never treats status rows as runnable work.

### Registry pack pre-expand enqueue

Called from `pkg/registry` when `PreExpandValueSets` and `JobStore` are configured:

```go
err := jobs.EnqueuePackPreExpand(ctx, jobStore, scopeID, packName, packVersion, tenantID, time.Now)
jobID := jobs.PackPreExpandJobID(scopeID, packName, packVersion)
_ = jobID
```

Pending or running jobs with the stable id are left untouched; completed/failed jobs reset to pending for re-install.

## Examples

**Build a job with JSON payload and fixed clock (from `jobs_test`):**

```go
fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
job, err := jobs.NewJob(jobs.TypeReindex, map[string]string{"resourceType": "Patient"}, jobs.EnqueueOptions{
    ID:  "job-1",
    Now: func() time.Time { return fixed },
})
if err != nil {
    return err
}
err = jobStore.Enqueue(ctx, job)
```

**Enqueue with tenant/principal ownership stamped on payload:**

```go
_, err := jobs.Enqueue(ctx, jobStore, jobs.TypeSyncRetryPush, payload, jobs.EnqueueOptions{
    PrincipalID: "user-abc",
    TenantID:    "tenant-1",
})
owner, ok := jobs.OwnerFromPayload(job.Payload)
_ = owner
_ = ok
```

**Retry, status lookup, and RunAfter (from `jobs_test`):**

```go
runner.MaxAttempts = 3
runner.Backoff = jobs.Backoff{Base: 10 * time.Second, Factor: 2, Max: time.Minute}

handleErr := handler.HandleJob(ctx, *claimed)
_ = jobs.ApplyHandlerResult(ctx, jobStore, claimed, handleErr, jobs.ApplyOptions{
    MaxAttempts: 3, Backoff: jobs.DefaultBackoff(), Now: time.Now,
})

payload, _ := jobs.Lookup[MyPayload](ctx, jobStore, jobs.TypeExportBulkRecord, exportID)
rec, getErr := jobs.GetRecord(ctx, jobStore, jobs.TypeSearchReindex, jobID)
_ = jobs.IsMissing(getErr)
_ = rec

_, _ = jobs.Enqueue(ctx, jobStore, "demo", nil, jobs.EnqueueOptions{
    ID: "future", RunAfter: now.Add(time.Hour), Now: func() time.Time { return now },
})
```

**Well-known types and registry integration:**

```go
_ = jobs.TypeSearchReindex // canonical; legacy: jobs.TypeReindex ("reindex")
_ = jobs.TypeTerminologyPreExpand
_ = jobs.RegistryPrincipalID
body, _ := jobs.MarshalPayload(map[string]string{"resourceType": "Patient"})
_ = body
```

## Configuration / key types

### `Runner`

| Field | Default | Role |
|-------|---------|------|
| `Store` | required | `store.JobStore` for claim/update |
| `MaxAttempts` | `1` | Terminal failure after N claims; `1` means no retry |
| `Backoff` | `DefaultBackoff()` | Used when `MaxAttempts > 1` |
| `PollInterval` | `1s` | Idle sleep in `RunLoop` |
| `Now` | UTC `time.Now` | Clock for updates and backoff |

### `EnqueueOptions`

| Field | Role |
|-------|------|
| `ID` | Stable job id; duplicates return `ErrDuplicateJob` |
| `RunAfter` | Delay claim until this timestamp |
| `Now` / `NewID` | Test hooks for timestamps and ids |
| `PrincipalID` / `TenantID` | Stamped into payload via `StampOwner` |

**Job types:** dotted prefixes in `types.go` — `sync.*`, `search.*` (plus legacy `reindex`), `view.*`, `analytics.*`, `export.*` (including separate `*.record` status types), `modules.install`, `registry.*` (package install, terminology install/pre-expand). See `doc.go` for the full list.

**Errors:** `ErrNilStore`, `ErrNilHandler`, `ErrEmptyJobType`, `ErrDuplicateJob`, `ErrUnknownJobType`, `ErrJobNotFound`, `ErrConcurrentUpdate`.

## Where it fits

| Package | Role |
|---------|------|
| **store** | `JobStore` persistence contract |
| **jobs** | Shared runtime (this package) |
| **sqlite** / **postgres** | Durable job backends |
| **search** | Reindex workers and enqueue |
| **sync** | Retry, pull, conflict, replay jobs |
| **registry** | Pre-expand enqueue; terminology install workers |
| **modules** | Async module directory install |
| **export** | Bulk export/import status rows |

## Limits

- **`MaxAttempts = 1`** (default) matches historical search/sync behavior: first handler error marks the job **failed** with no backoff.
- **`RunLoop` swallows handler errors** and continues polling unless the context is cancelled; use `RunOnce` for fail-fast tests.
- **Registration order** determines `RunOnce` claim order across types; re-registering a type replaces the handler but preserves first-registration order.
- **StatusStore.Update** uses in-process mutex plus optional `JobCASStore` compare-and-swap on `UpdatedAt` for cancel-wins guards.
- **Job types are not validated at enqueue** — a typo means the job may sit pending forever unless a handler is registered.
- **Terminology workers** (`terminology_install_worker.go`, `terminology_pre_expand_worker.go`) are thin adapters; heavy logic remains in `pkg/terminology` and `pkg/registry`.
- **`MapBulkRecordStatus`** maps FHIR bulk status strings to `store.JobStatus` for status rows only.

## Related docs

- [doc.go](./doc.go) — package comment and API index
- [docs/architecture.md](../../docs/architecture.md) — background processing overview
- [pkg/store/README.md](../store/README.md) — `JobStore` contract
- [pkg/search/README.md](../search/README.md) — reindex integration
- [pkg/sync/README.md](../sync/README.md) — sync job handlers
- [pkg/registry/README.md](../registry/README.md) — package install and pre-expand triggers
- [pkg/sqlite/README.md](../sqlite/README.md) / [pkg/postgres/README.md](../postgres/README.md) — durable backends
- [docs/bulk-data-verification.md](../../docs/bulk-data-verification.md) — bulk export/import flows using status rows
