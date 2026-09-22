# haistack-hooks (`pkg/hooks`)

Small **FHIR intercept SPI** for HTTP and core write paths.

## What it does

HAPI-style interceptor buses expose dozens of pointcuts. HAIStack keeps **four** intentional extension points:

| Pointcut | When it runs |
|----------|----------------|
| `Incoming` | HTTP request routed, before handler |
| `PreStorage` | Core write, after validation/id assignment, before persist |
| `PostCommit` | Core write, after `WriteSession` commits |
| `Outgoing` | HTTP response, before resource envelope is serialized |

Register `Func` values on a `Registry` with `On`, then wire the same registry into:

- `core.ResourceServiceConfig.Hooks`
- `http.Config.Hooks`

`runtime.Builder.WithHooks` sets both.

Hooks run in **registration order**. Errors from Incoming, PreStorage, or Outgoing **abort** the request. PostCommit errors are **ignored** so a successful write is not reported as failure.

## When to use it

- **Audit or metrics** on every write without forking `pkg/core`  
- **Enrichment** — add derived fields in PreStorage (keep idempotent)  
- **Response shaping** — strip internal extensions on Outgoing  
- **Request guards** — custom headers or tenancy checks on Incoming  

For transport-wide concerns (CORS, rate limits), prefer standard HTTP middleware **outside** this SPI.

## Usage

```go
import (
    "github.com/degoke/haistack/pkg/hooks"
)

reg := hooks.NewRegistry()
reg.On(hooks.PreStorage, func(ctx hooks.Context) error {
    // mutate ctx envelope or return error to fail write
    return nil
})

svc, _ := core.NewResourceService(core.ResourceServiceConfig{
    // ...
    Hooks: reg,
})

handler, _ := haihttp.NewHandler(haihttp.Config{
    // ...
    Hooks: reg,
})
```

Or:

```go
rt, err := runtime.New().
    WithSQLite(path).
    WithHooks(reg).
    Build(ctx)
```

## Where it fits

```text
HTTP ──► Incoming ──► handler ──► core ──► PreStorage ──► WriteSession ──► PostCommit
                                                                              │
HTTP ◄── Outgoing ◄── envelope ◄────────────────────────────────────────────┘
```

Do **not** add new pointcut types in this package—extend behaviour by registering another function on one of the four, or wrap middleware at the HTTP server level.

## Limits

- PostCommit failures are swallowed by design—use reliable side-effect queues if you must not lose work.  
- Hooks are synchronous; long-running work belongs in `pkg/jobs` triggered from PostCommit with care.

## Related docs

- [pkg/core/README.md](../core/README.md)  
- [pkg/http/README.md](../http/README.md)
