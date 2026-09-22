# haistack-hooks (`pkg/hooks`)

Small **FHIR intercept SPI** for HTTP and core write paths.

Four intentional extension points (not a full HAPI interceptor bus).

---

## What it does

**Points** (`hooks.go`):

| Point | Constant | When |
|-------|----------|------|
| Incoming | `hooks.Incoming` | HTTP routed, before handler |
| PreStorage | `hooks.PreStorage` | Core write, before persist |
| PostCommit | `hooks.PostCommit` | After successful `WriteSession` commit |
| Outgoing | `hooks.Outgoing` | Before response envelope serialized |

**Event:**

```go
type Event struct {
    Point        Point
    Action       Action
    ResourceType string
    ID           string
    Operation    string
    Resource     *types.ResourceEnvelope // mutable: PreStorage, Outgoing
    Previous     *types.ResourceEnvelope
}
```

**Actions:** `ActionRead`, `ActionCreate`, `ActionUpdate`, `ActionPatch`, `ActionDelete`, `ActionSearch`, `ActionHistory`, `ActionTransaction`, `ActionBatch`, `ActionOperation`, `ActionMetadata`.

**Registry:**

- `NewRegistry()`, `On(point, Func)`, `Run(ctx, point, event)`
- Registration order preserved; first error stops later funcs on that point (`TestRegistryRunsInOrderAndStopsOnError`)
- Unknown points rejected (`TestRegistryRejectsUnknownPoint`)
- Nil registry `Run` → noop

**Failure semantics:**

- Incoming / PreStorage / Outgoing errors **abort** the operation
- PostCommit errors **ignored** in core (`_ = s.hooks.Run(...)`) — `TestPostCommitHookSeesPersistedResource`

Wire the same `hooks.Hooks` into `core.ResourceServiceConfig.Hooks` and `http.Config.Hooks`, or `runtime.Builder.WithHooks`.

Do **not** add new point types here — use HTTP middleware for CORS/rate limits.

---

## How it fits in the ecosystem

```text
HTTP ──► Incoming (pkg/http) ──► handler ──► core
                                                  │
                                            PreStorage
                                                  │
                                            WriteSession
                                                  │
                                            PostCommit (errors ignored)
                                                  │
HTTP ◄── Outgoing ◄── formattedResponseWriter
```

Incoming/Outgoing are HTTP-aware (`actionFromRoute` in `http/hooks.go`). PreStorage/PostCommit are write-path only.

---

## When to use it

- Audit/metrics on writes
- PreStorage enrichment (keep idempotent)
- Outgoing redaction/strip internal extensions
- Incoming guards (maintenance mode, custom headers)
- Domain rejection via `*core.ServiceError` from PreStorage

Avoid long work in hooks; use [`pkg/jobs`](../jobs/README.md) from PostCommit if needed. Do not rely on PostCommit for must-not-lose side effects.

---

## Usage modes

### 1. Runtime builder

```go
reg := hooks.NewRegistry()
_ = reg.On(hooks.PreStorage, func(ctx context.Context, event *hooks.Event) error {
    return nil
})
rt, err := runtime.New().WithSQLite(path).WithHooks(reg).Build(ctx)
```

### 2. Manual core + HTTP

```go
reg := hooks.NewRegistry()
_ = reg.On(hooks.Incoming, func(ctx context.Context, event *hooks.Event) error {
    return nil
})
svc, _ := core.NewResourceService(core.ResourceServiceConfig{Hooks: reg, /* stores */})
handler, _ := haihttp.NewHandler(haihttp.Config{Hooks: reg, /* ... */})
```

### 3. PreStorage mutate or reject

From `core/hooks_test.go`:

```go
_ = reg.On(hooks.PreStorage, func(_ context.Context, event *hooks.Event) error {
    if strings.Contains(string(event.Resource.JSON), "Blocked") {
        return errors.New("blocked")
    }
    event.Resource.JSON = []byte(`{"resourceType":"Patient","id":"pat-1",...}`)
    return nil
})
```

### 4. PostCommit (non-fatal)

```go
_ = reg.On(hooks.PostCommit, func(_ context.Context, event *hooks.Event) error {
    return errors.New("ignored") // write still succeeds
})
```

### 5. Outgoing shaping

HTTP `formattedResponseWriter` runs Outgoing before serialize (`http/hooks_test.go`, `writer.go`).

---

## Examples (APIs from this repo)

```go
err := reg.On(hooks.Incoming, fn)
err = reg.Run(ctx, hooks.Incoming, &hooks.Event{
    Action: hooks.ActionRead, ResourceType: "Patient", ID: "p1",
})
```

**Func signature:**

```go
type Func func(ctx context.Context, event *hooks.Event) error
```

**Mutate resource** (`TestRegistryMutatesEventResource`): replace `event.Resource` in PreStorage.

**Incoming non-ServiceError** → HTTP `invalidRequest("incoming hook rejected request", err)`.

**Ordering:** hooks `a`, `b`, `c` on Incoming — if `b` errors, `c` skipped.

---

## Where it fits

```text
HTTP ──► Incoming ──► handler ──► core ──► PreStorage ──► WriteSession ──► PostCommit
                                                                              │
HTTP ◄── Outgoing ◄── envelope ◄────────────────────────────────────────────┘
```

One registry instance keeps HTTP and core policy aligned.

---

## Limits

- Four points only — no ad-hoc pointcut constants
- PostCommit failures swallowed — use durable queues
- Synchronous — blocks request path
- PreStorage/PostCommit not on read-only core paths (reads use HTTP Incoming/Outgoing)
- Register hooks at startup; `Registry` is mutex-safe for concurrent `Run`
- Raw error responses may bypass Outgoing resource mutation

---

## Related docs

- [pkg/core/README.md](../core/README.md)
- [pkg/http/README.md](../http/README.md)
- [pkg/runtime/README.md](../runtime/README.md)
- [pkg/jobs/README.md](../jobs/README.md)
