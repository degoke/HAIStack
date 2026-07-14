// Package jobs implements haistack-jobs, the shared background job runtime for
// Health AI Stack.
//
// # Scope
//
// v1 builds on store.JobStore as the persistence contract. This package owns
// the shared runtime layer: handler registration, claim/dispatch loops,
// retry/backoff helpers, enqueue conventions, and an in-memory JobStore for
// tests and local development.
//
// It does not replace store.JobStore. Postgres persistence remains in
// pkg/postgres (TenantDB.JobStore). SQLite persistence is in pkg/sqlite
// (DB.JobStore). Callers enqueue through those adapters (or InMemoryJobStore)
// and process work through Runner.
//
// Typical consumers:
//
//   - pkg/search — reindex jobs (canonical "search.reindex"; legacy "reindex")
//   - pkg/sync — retry, pull, conflict, and replay jobs (sync.* types)
//   - future view refresh, analytics, binary cleanup, subscriptions, exports
//
// # Public API
//
//   - Handler / HandlerFunc — process one claimed JobRecord
//   - Runner — Register, RunOnce, RunLoop against a store.JobStore
//   - Enqueue / NewJob / MarshalPayload — typed payload helpers with defaults
//   - Backoff / NextRunAfter / ApplyHandlerResult — retry and status helpers
//   - InMemoryJobStore — concurrent-safe store.JobStore for tests/dev
//
// # Job type conventions
//
// Prefer dotted names scoped by owning package (for example "sync.retry_push",
// "search.reindex"). Legacy short names such as search's "reindex" remain
// valid for compatibility. Type strings are opaque to this package; only
// registered handlers receive claimed jobs.
//
// # Claim semantics
//
// ClaimNext implementations (including InMemoryJobStore and pkg/sqlite) must:
//
//   - select the oldest pending job of the requested type whose RunAfter is
//     zero or not after now
//   - transition status to running
//   - increment Attempts atomically with the claim
//
// Runner then applies success, retry (pending + RunAfter), or terminal failure.
package jobs
