# haistack-jobs (`pkg/jobs`)

Shared background job runtime for Health AI Stack. Builds on
`store.JobStore` — it does not replace the persistence contract.

## What it does

- Canonical job type conventions and enqueue helpers
- `Handler` / `Runner` for claim + dispatch by `job.Type`
- Retry/backoff and status transition helpers
- `InMemoryJobStore` for tests and local development

Postgres persistence: `postgres.TenantDB.JobStore()`.
SQLite persistence: `sqlite.DB.JobStore()`.

Preferred dotted job types live alongside legacy names for compatibility. For
example, search may use canonical `search.reindex` (`jobs.TypeSearchReindex`)
or legacy `reindex` (`jobs.TypeReindex`).

## Usage

```go
jobsStore := jobs.NewInMemoryJobStore() // or sqlite/postgres JobStore

runner := jobs.NewRunner(jobsStore)
runner.MaxAttempts = 5
_ = runner.Register(jobs.TypeReindex, jobs.HandlerFunc(worker.HandleJob))

job, err := jobs.Enqueue(ctx, jobsStore, jobs.TypeReindex, payload, jobs.EnqueueOptions{})
processed, err := runner.RunOnce(ctx)
// or: go runner.RunLoop(ctx)
```

## Claim semantics

Claim selects the oldest pending job of the requested type whose `RunAfter` is
empty or due, sets status to `running`, and increments `Attempts`.

## Where it fits

| Package | Role |
|---------|------|
| **store** | `JobStore` persistence contract |
| **jobs** | Shared runtime (this package) |
| **search** | Reindex via `Runner` / enqueue helpers |
| **sync** | Retry/conflict/pull jobs via shared runtime |
| **postgres** / **sqlite** | Durable backends |

See [doc.go](./doc.go) for the full API.
