# haistack-audit (`pkg/audit`)

Shared audit event library for HAIStack.

## What it does

**haistack-audit** defines the **canonical audit event model** and **emit helpers** used across auth, FHIR reads/writes, sync, AI tooling, views, exports, blob access, and terminology translation. It builds on `store.AuditStore` — the persistence contract — without owning storage itself.

Producers call typed helpers such as `LogResourceRead`, `LogAuthDecision`, or `LogSyncEvent` with small event structs; helpers normalize default outcomes, merge detail maps, and map to the shared **`Event`** shape. Append goes through the **`Logger`** interface, most often **`StoreAdapter`** wrapping `store.AuditStore`.

Querying uses **`Query`** (package-level filters) converted to `store.AuditQuery` in `StoreAdapter.ListEvents`, with **`FromStoreRecord` / `ToStoreRecord`** bridging legacy detail keys and first-class columns.

An optional **`MemoryStore`** implements `store.AuditStore` for tests. **`NopLogger`** discards events when auditing is disabled.

## What it does not do

- **Choose authorization policy** — `pkg/auth` decides allow/deny; audit records the decision
- **Open databases or define schemas** — use `pkg/sqlite` or `pkg/postgres` for `AuditStore`
- **Replace package-specific seams** — `ai.AuditLogger` and `view.AuditLogger` adapt into these helpers rather than duplicating event shapes
- **Guarantee tamper-evident or WORM storage** — append semantics depend on the backend implementation
- **Correlate logs across services** — events are row-oriented; distributed tracing is out of scope

Audit owns **consistent event vocabulary and append/query adapters**, not **policy or storage engines**.

## How it fits in the ecosystem

```
  pkg/auth / pkg/core / pkg/sync / pkg/ai / pkg/view / pkg/export
                              |
                    Log* helpers (emit.go)
                              |
                              v
                         audit.Logger
                              |
                    StoreAdapter.Log -> Event
                              |
                              v
                      store.AuditStore
                              |
                    sqlite.DB / postgres.TenantDB
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **store** | `AuditStore`, `AuditRecord`, `AuditQuery` |
| Upstream | **sqlite** / **postgres** | Durable audit persistence |
| Peer | **auth** | Emits `LogAuthDecision`; does not own storage |
| Peer | **sync** | Uses `LogSyncEvent` and sync/conflict action constants |
| Peer | **ai** | Tool/model invoke events via adapters |
| Peer | **view** | `LogViewAccess` for execute-view auditing |
| Peer | **export** | `LogExport` for export operations |
| Downstream | **operators / compliance** | Query via `ListEvents` or store APIs |

Typical wiring in a server: construct `auditStore := tenantDB.AuditStore()`, wrap with `&audit.StoreAdapter{Store: auditStore}`, pass the adapter (as `audit.Logger`) into HTTP middleware and domain services.

## When to use it

- Recording FHIR resource reads and writes with actor, tenant, and outcome
- Auditing authorization allow/deny decisions with reasons and scoped targets
- Logging sync acceptance, rejection, conflicts, and device push/pull
- Logging AI tool execution and model invocation with conversation metadata
- Logging view execution, CSV/bulk export, and binary/blob access
- Logging ConceptMap translation for terminology provenance research
- Unit tests that assert action names and fields without a database (`NewMemoryStore`)

## Usage modes

### Production append via StoreAdapter

Bridge domain code to durable storage:

```go
import (
    "context"

    "github.com/degoke/haistack/pkg/audit"
)

auditStore := tenantDB.AuditStore()
logger := &audit.StoreAdapter{Store: auditStore}

err := audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{
    Actor:      "user-1",
    Tenant:     "tenant-a",
    Subject:    "patient/pat-1",
    AuthAction: "read",
    Allowed:    true,
    Reason:     "rule matched",
})
```

### In-memory tests without SQLite

Use `MemoryStore` plus `StoreAdapter` for round-trip assertions:

```go
mem := audit.NewMemoryStore()
logger := &audit.StoreAdapter{
    Store: mem,
    Now:   func() time.Time { return fixedTime },
    NewID: func() string { return "fixed-id" },
}
```

### Disabled auditing in hot paths

Inject `audit.NopLogger{}` or a no-op `LoggerFunc` when configuration turns auditing off; emit helpers return `ErrNilLogger` only when the logger is nil.

### Query and investigation

List canonical events with package-level filters:

```go
events, err := logger.ListEvents(ctx, audit.Query{
    Actor:  "user-1",
    Action: audit.ActionResourceRead,
    Limit:  100,
})
```

Lower-level store access remains available via `StoreAdapter.List` with `store.AuditQuery`.

## Examples

**FHIR read/write and sync (from `audit_test` patterns):**

```go
_ = audit.LogResourceRead(ctx, logger, audit.ResourceReadEvent{
    Actor: "practitioner-1", Tenant: "tenant-a", ResourceType: "Patient", ResourceID: "pat-1",
})
_ = audit.LogResourceWrite(ctx, logger, audit.ResourceWriteEvent{
    Actor: "practitioner-1", ResourceType: "Observation", ResourceID: "obs-1", Operation: "create",
})
_ = audit.LogSyncEvent(ctx, logger, audit.SyncEvent{
    Actor: "device-9", Action: audit.ActionSyncAccepted, Outcome: audit.OutcomeSuccess,
})
```

**AI, auth, view, export, blob, terminology:**

```go
_ = audit.LogAIToolCall(ctx, logger, audit.AIToolCallEvent{
    Actor: "agent-1", ToolName: "read_fhir_resource", Outcome: audit.OutcomeSuccess,
    ConversationID: "conv-1",
})
_ = audit.LogAIModelInvoke(ctx, logger, audit.AIToolCallEvent{
    Actor: "agent-1", ToolName: "stub-v1", Outcome: audit.OutcomeSuccess,
})
_ = audit.LogAuthDecision(ctx, logger, audit.AuthDecisionEvent{
    Actor: "user-2", Allowed: false, Reason: "insufficient scope",
})
_ = audit.LogViewAccess(ctx, logger, audit.ViewAccessEvent{
    Actor: "analyst-1", ViewName: "patient-summary", Version: "2",
})
_ = audit.LogExport(ctx, logger, audit.ExportEvent{Actor: "analyst-1", ViewName: "cohort-export"})
_ = audit.LogBlobAccess(ctx, logger, audit.BlobAccessEvent{Actor: "service-1", BlobKey: "binary/doc-1"})
_ = audit.LogTerminologyTranslate(ctx, logger, audit.TerminologyTranslateEvent{
    MapURL: "http://example.org/ConceptMap/example", SourceCode: "123", TargetCode: "456",
})
```

**Round-trip and custom logger:**

```go
events, err := logger.ListEvents(ctx, audit.Query{Actor: "agent-1", Action: audit.ActionExecuteTool})
rec := audit.ToStoreRecord(audit.Event{Actor: "system", Action: audit.ActionExport})
ev := audit.FromStoreRecord(rec)
_ = events
_ = err
_ = ev
```

## Configuration / key types

**`Event`:** `ID`, `Timestamp`, `Actor`, `Tenant`, `Subject`, `Action`, `Outcome`, resource/view/tool fields, `BlobKey`, `Details`. Helpers set subsets; `StoreAdapter` fills zero `Timestamp`/`ID`.

**`Query`:** filters for `ListEvents` (resource, actor, action, outcome, tenant, view, tool, conversation, time range, limit).

**`StoreAdapter`:** `Store` (required), optional `Now` / `NewID` for tests.

**Actions / outcomes:** defined in `types.go` — prefer `ActionResourceRead`, `ActionAuthAllow`/`ActionAuthDeny`, `ActionSync*`, `ActionConflict*`, `ActionExecuteTool`, `ActionExecuteView`, `ActionInvokeModel`, `ActionExport`, `ActionBlobAccess`, `ActionTerminologyTranslate`; outcomes include `OutcomeSuccess`, `OutcomeError`, `OutcomeAllow`/`OutcomeDeny`, etc.

**Errors:** `ErrNilStore`, `ErrNilLogger`.

## Where it fits

| Package | Role |
|---------|------|
| **store** | `AuditStore` persistence contract |
| **audit** | Canonical events and helpers (this package) |
| **sqlite** / **postgres** | Durable audit backends |
| **auth** | Policy decisions; optional emit via auditing engine |
| **sync** | Device and conflict auditing |
| **ai** | Tool/model invoke adapters |
| **view** | View execution auditing |
| **export** | Export operation auditing |
| **terminology** | Translation audit for research/provenance |

## Limits / MVP notes

- **v1 focuses on append and list** — retention, encryption, and streaming export are backend/ops concerns.
- **Legacy detail keys** — `FromStoreRecord` lifts `tenant`, `subject`, `viewName`, etc. from `Details` when first-class columns are empty.
- **Helper defaults** — empty outcomes on read/write/export/blob default to `OutcomeSuccess`; auth uses allow/deny actions and outcomes explicitly.
- **`LogSyncEvent` does not validate** `Action` — callers must pass one of the documented sync/conflict/device constants.
- **`MemoryStore.List`** applies simple in-memory filtering; it is not a production backend.
- **Nil logger is an error** — use `NopLogger` instead of nil when auditing is disabled.
- **Query support varies by backend** — prefer fields mirrored in `store.AuditQuery`; unsupported filters may be no-ops on thin adapters.

## Related docs

- [doc.go](./doc.go) — package comment and API index
- [docs/architecture.md](../../docs/architecture.md) — cross-cutting concerns
- [docs/smart-auth-architecture.md](../../docs/smart-auth-architecture.md) — auth and auditing boundaries
- [pkg/store/README.md](../store/README.md) — `AuditStore` contract
- [pkg/auth/README.md](../auth/README.md) — authorization and optional audit emit
- [pkg/sync/README.md](../sync/README.md) — sync action constants usage
- [pkg/ai/README.md](../ai/README.md) — AI audit adapters
- [pkg/view/README.md](../view/README.md) — view execution and auditing
- [pkg/sqlite/README.md](../sqlite/README.md) / [pkg/postgres/README.md](../postgres/README.md) — durable backends
