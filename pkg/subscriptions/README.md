# haistack-subscriptions (`pkg/subscriptions`)

Tenant-neutral event automation on top of `store.EventStore` change events and `pkg/jobs` background delivery.

## What it does

**haistack-subscriptions** is a **downstream consumer** of the existing resource event log. It registers triggers on resource type and event kind, matches incoming `store.ResourceEvent` entries, and enqueues durable delivery jobs. It does not hook into `pkg/core` write paths directly.

Capabilities:

- **Internal trigger model** — resource type + event (`create`, `update`, `delete`, or `change` for create+update), optional changed-field filters, optional FHIRPath predicates, optional search criteria
- **FHIR adapter** — `RegisterFromFHIRSubscription` maps a supported subset of FHIR `Subscription` (rest-hook only) into the internal model
- **Event processor** — reads `EventStore` since a `CursorStore` checkpoint, matches active subscriptions, enqueues delivery jobs
- **Durable delivery** — webhook (HTTP POST/PUT) and local (in-process handler) channels
- **Retry + logging** — retries via `pkg/jobs`; operational delivery logs in `subscription_delivery_log`

Core types:

| Type | Purpose |
|------|---------|
| `Manager` | Register, update, disable, list, delete subscription records |
| `Processor` | Consume `ResourceEvent` entries and schedule delivery work |
| `DeliveryWorker` | Execute queued deliveries |
| `DeliveryJobRunner` | Claim and run delivery jobs via `pkg/jobs` |
| `Matcher` | Evaluate triggers against current/previous resource state |
| `WebhookDispatcher` | HTTP POST/PUT transport |
| `LocalDispatcher` / `HandlerRegistry` | In-process handler transport |

Job type: `jobs.TypeSubscriptionsDeliver` (`subscriptions.deliver`).

It does **not** (v1):

- WebSocket, email, SMS, or message channel types
- Dead-letter queues or full delivery audit expansion
- Kafka/NATS or a separate queue service
- Tenant semantics in the core API (Postgres scoping is via `TenantDB` wiring)
- Chained, `_include`, `_revinclude`, full-text, modifiers (`:not`, `:exact`), or comparator prefixes in FHIR `Subscription.criteria`
- Delete notifications from FHIR `Subscription.criteria` (use internal `TriggerEventDelete` if needed)

## How it fits in the ecosystem

```
pkg/core (write)  →  EventStore (outbox / event_log)
                           |
                           v
              subscriptions.Processor (Matcher + cursor)
                           |
                           v
                     JobStore (subscriptions.deliver)
                           |
                           v
              subscriptions.DeliveryWorker
                    /              \
         WebhookDispatcher    LocalDispatcher
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Upstream | **core** | Produces `ResourceEvent` on writes (unchanged) |
| Upstream | **store** | Event, cursor, job, subscription, resource, history stores |
| Upstream | **fhirpath** | `FilterFHIRPath` evaluation in `Matcher` |
| Upstream | **search** | `ParseQuery`, `MatchResourceParameter`, criteria params |
| Downstream | **jobs** | Durable queue and retry runtime |
| Sidecar | **runtime** | Wires default matcher with engine + registry |

## When to use it

- **Webhook notifications** when FHIR resources change (REST-hook style)
- **In-process reactions** — register a named local handler for create/update/delete
- **Filtered triggers** — e.g. `Observation.created` where `code = X` via FHIRPath
- **Field-scoped updates** — e.g. only when `Appointment.status` changed
- **Edge (SQLite)** or **hub (Postgres)** — same package, different store wiring
- **FHIR Subscription compatibility** for simple equality criteria (`Patient?active=true`)

## Usage modes

### 1. Register and manage subscriptions (`Manager`)

```go
mgr := &subscriptions.Manager{Store: db.SubscriptionStore()}

rec, err := mgr.Register(ctx, "patient-created",
    subscriptions.Trigger{
        ResourceType: "Patient",
        Event:        subscriptions.TriggerEventCreate,
    },
    subscriptions.Channel{
        Type: subscriptions.ChannelTypeWebhook,
        Webhook: &subscriptions.WebhookConfig{
            URL:    "https://example.test/hooks/patient",
            Method: "POST",
        },
    },
    subscriptions.RetryPolicy{MaxAttempts: 5},
)
```

Also: `Update`, `Disable`, `Enable`, `List`, `Get`, `Delete` on the manager.

### 2. Field-scoped and FHIRPath triggers

```go
// Appointment.status changed
subscriptions.Trigger{
    ResourceType:  "Appointment",
    Event:         subscriptions.TriggerEventUpdate,
    ChangedFields: []string{"status"},
}

// Observation.created where code = 8867-4
subscriptions.Trigger{
    ResourceType:   "Observation",
    Event:          subscriptions.TriggerEventCreate,
    FilterFHIRPath: "code.coding.code = '8867-4'",
}
```

Changed-field matching compares top-level JSON fields between previous history snapshot and current resource.

### 3. Run the event processor (batch or loop)

```go
engine, _ := fhirpath.NewEngine(fhirpath.Config{})

processor := &subscriptions.Processor{
    Events:        db.OutboxStore(),
    Cursors:       db.CursorStore(),
    Subscriptions: db.SubscriptionStore(),
    Jobs:          db.JobStore(),
    Resources:     db.ResourceStore(),
    History:       db.HistoryStore(),
    Matcher:       &subscriptions.Matcher{Engine: engine, Registry: searchRegistry},
    Scope:         "default",
}

n, err := processor.RunOnce(ctx)
go processor.RunLoop(ctx, time.Second)
```

Checkpoint: `subscriptions.CursorName(scope)` → `subscriptions.processor.{scope}`.

`Matcher.Registry` is **required** when triggers use `FilterParams` or FHIR criteria; nil registry returns `ErrNilRegistry`.

### 4. Delivery workers and local handlers

```go
registry := subscriptions.NewHandlerRegistry()
registry.Register("on-patient-created", func(ctx context.Context, payload subscriptions.DeliverPayload, resourceJSON []byte, metadata map[string]any) error {
    return nil
})

worker := &subscriptions.DeliveryWorker{
    Subscriptions: db.SubscriptionStore(),
    Deliveries:    db.SubscriptionDeliveryStore(),
    Resources:     db.ResourceStore(),
    Webhook:       &subscriptions.WebhookDispatcher{},
    Local:         &subscriptions.LocalDispatcher{Registry: registry},
}

runner := &subscriptions.DeliveryJobRunner{
    Jobs:        db.JobStore(),
    Worker:      worker,
    MaxAttempts: 5,
}
_, err := runner.RunOnce(ctx)
```

Delivery job IDs: `subscriptions:deliver:{subscriptionId}:{eventSequence}` (idempotent re-processing).

### 5. FHIR Subscription adapter

```go
rec, err := mgr.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
    Status:   "active",
    Criteria: "Patient?active=true",
    Channel: subscriptions.FHIRSubscriptionChannel{
        Type:     "rest-hook",
        Endpoint: "https://example.test/hook",
        Payload:  "application/fhir+json",
    },
}, extensions)
```

Parse from FHIR JSON:

```go
input, extensions, err := subscriptions.ParseFHIRSubscriptionJSON(subscriptionJSON)
```

Unsupported shapes return `ErrUnsupportedFHIR` (modifiers, chains, websocket channel, etc.).

### 6. Search-criteria triggers (create + update)

```go
parsed, _ := search.ParseQuery("Observation", url.Values{"code": []string{"8867-4"}})
trigger := subscriptions.Trigger{
    ResourceType: "Observation",
    Event:        subscriptions.TriggerEventChange,
    Criteria:     "Observation?code=8867-4",
    FilterParams: parsed.Params,
}
```

Internal `TriggerEventChange` matches create and update; FHIR adapter uses the same semantics for criteria subscriptions.

## Examples

**Disable a subscription without deleting:**

```go
err := mgr.Disable(ctx, rec.ID)
```

**Webhook with custom headers:**

```go
subscriptions.WebhookConfig{
    URL:     "https://hooks.example/clinical",
    Method:  "POST",
    Headers: map[string]string{"X-Tenant": "a"},
}
```

**Inspect delivery log after failure:**

```go
deliveries, err := db.SubscriptionDeliveryStore().ListBySubscription(ctx, rec.ID, limit)
```

## Storage

Contracts in `pkg/store`:

| Interface | Role |
|-----------|------|
| `SubscriptionStore` | Registry CRUD |
| `SubscriptionDeliveryStore` | Delivery attempt log |
| `CursorStore` | Processor checkpoint (reused) |
| `JobStore` | Delivery work + retries (reused) |

| Backend | Tables | Accessors |
|---------|--------|-----------|
| SQLite | `subscription_registry`, `subscription_delivery_log` | `sqlite.DB.SubscriptionStore()`, `SubscriptionDeliveryStore()` |
| Postgres | same + `tenant_id` | `postgres.TenantDB.SubscriptionStore()`, `SubscriptionDeliveryStore()` |

## Configuration / key types

| Type | Notes |
|------|-------|
| `TriggerEvent` | `create`, `update`, `delete`, `change` |
| `ChannelType` | `webhook`, `local` |
| `SubscriptionRecord` | ID, name, status, trigger, channel, retry policy, timestamps |
| `DeliverPayload` | Subscription id, event sequence, resource type/id, operation |
| `RetryPolicy` | `MaxAttempts` (defaults applied when <= 0) |

**Errors:** `ErrNilStore`, `ErrNilEngine`, `ErrNilRegistry`, `ErrNotFound`, `ErrInvalidTrigger`, `ErrInvalidChannel`, `ErrUnsupportedFHIR`, `ErrUnknownHandler`, `ErrDuplicateDelivery`.

## Where it fits

| Package | Role |
|---------|------|
| **store** | Event, cursor, job, subscription stores |
| **jobs** | Durable delivery queue and retry runtime |
| **fhirpath** | In-resource filter evaluation |
| **search** | Criteria parsing and parameter matching |
| **core** | Produces events; subscriptions optional at deploy time |
| **sqlite** / **postgres** | Persistence backends |

## Limits

- FHIR adapter: rest-hook only; criteria equality parameters without modifiers
- No delete notifications for FHIR criteria subscriptions
- Local handler names are in-memory only (`HandlerRegistry`)
- Processor scope is a string partition for cursors — not a security boundary
- Webhook dispatcher uses standard library HTTP client; mTLS and signing are caller responsibilities
- Dead-letter and expanded audit trails deferred

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — event-driven automation
- [pkg/core/README.md](../core/README.md) — event emission on write
- [pkg/jobs/README.md](../jobs/README.md) — delivery job runner
- [pkg/search/README.md](../search/README.md) — criteria parsing and matching
- [pkg/fhirpath/README.md](../fhirpath/README.md) — predicate evaluation
- [pkg/store/README.md](../store/README.md) — subscription store contracts
- [doc.go](./doc.go) — package entry point and trigger examples
