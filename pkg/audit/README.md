# haistack-audit (`pkg/audit`)

Shared audit event library for Health AI Stack. Builds on
`store.AuditStore` — it does not replace the persistence contract.

## What it does

- Canonical `Event` model (actor, tenant, subject, action, outcome, targets)
- Shared action names for resource, auth, AI, view, sync, export, and blob
- `Logger` / `StoreAdapter` for append and query
- Emit helpers used by stack packages

Postgres persistence: `postgres.TenantDB.AuditStore()`.
SQLite persistence: `sqlite.DB.AuditStore()`.

Auth may emit decisions through this library; auth does **not** own audit
storage.

## Usage

```go
logger := &audit.StoreAdapter{Store: auditStore}

_ = audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{
    Actor: "user-1", Tenant: "tenant-a", Allowed: true, Reason: "rule matched",
})

_ = audit.LogAIToolCall(ctx, logger, audit.AIToolCallEvent{
    Actor: "agent-1", ToolName: "read_fhir_resource", Outcome: audit.OutcomeSuccess,
})
```

## Action names

| Constant | Value |
|----------|-------|
| `ActionResourceRead` | `resource.read` |
| `ActionResourceWrite` | `resource.write` |
| `ActionExecuteTool` | `execute-tool` |
| `ActionExecuteView` | `execute-view` |
| `ActionAuthAllow` / `ActionAuthDeny` | `auth.allow` / `auth.deny` |
| Sync/conflict | `sync.*` / `conflict.*` |
| `ActionExport` / `ActionBlobAccess` | `export` / `blob.access` |

## Where it fits

| Package | Role |
|---------|------|
| **store** | `AuditStore` persistence contract |
| **audit** | Canonical events and helpers (this package) |
| **auth** | Optional emit via `AuditingEngine` |
| **ai** / **view** | Adapters forward package seams into `audit` |
| **sync** | Action constants + `LogSyncEvent` |

See [doc.go](./doc.go) for the full API.
