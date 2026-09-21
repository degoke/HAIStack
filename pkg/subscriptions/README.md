# haistack-subscriptions (`pkg/subscriptions`)

Tenant-neutral event automation on top of `store.EventStore` change events and
`pkg/jobs` background delivery. Subscriptions are a **downstream consumer** of
the existing event log — they do not hook into `pkg/core` writes.

## What it does

- **Internal trigger model** — register subscriptions on resource type + event
  (`create`, `update`, `delete`, or `change` for create+update), optional
  changed-field filters, and optional FHIRPath predicates
- **FHIR adapter** — mapping from a supported subset of FHIR
  `Subscription` resources into the internal model (`RegisterFromFHIRSubscription`).
  Channel type is **rest-hook only**. `Subscription.criteria` is parsed with
  `search.ParseQuery` (for example `Patient?active=true`) and notifies on
  **create and update** of matching resources, not delete.
- **Event processor** — reads `EventStore` since a `CursorStore` checkpoint,
  matches active subscriptions, and enqueues delivery jobs
- **Durable delivery** — webhook (HTTP) and local (in-process handler) channels
- **Retry + logging** — retries via `pkg/jobs`; operational delivery logs in
  `subscription_delivery_log`

It does **not** (v1):

- WebSocket, email, SMS, or message channel types (FHIR adapter is rest-hook only)
- Dead-letter queues or full delivery audit expansion
- Kafka/NATS or a separate queue service
- Tenant semantics in the core API (Postgres scoping is via `TenantDB` wiring)
- Chained, `_include`, `_revinclude`, full-text, modifiers (`:not`, `:exact`),
  or comparator prefixes in `Subscription.criteria`
- Delete notifications from FHIR `Subscription.criteria` (use an internal
  `TriggerEventDelete` subscription if you need deletes)

```
pkg/core (write)  →  EventStore  →  subscriptions.Processor  →  JobStore
                                                                    ↓
                                              subscriptions.DeliveryWorker
                                                    ↓           ↓
                                            WebhookDispatcher  LocalDispatcher
```

## Core types

| Type | Purpose |
|------|---------|
| `Manager` | Register, update, disable, list, delete subscription records |
| `Processor` | Consume `ResourceEvent` entries and schedule delivery work |
| `DeliveryWorker` | Execute queued deliveries through `pkg/jobs` |
| `Matcher` | Evaluate triggers against current/previous resource state |
| `WebhookDispatcher` | HTTP POST/PUT transport |
| `LocalDispatcher` | In-process handler transport via `HandlerRegistry` |

| Record | Fields |
|--------|--------|
| `SubscriptionRecord` | ID, name, status, trigger, channel, retry policy, timestamps |
| `Trigger` | `ResourceType`, `Event` (`create`/`update`/`delete`/`change`), optional `ChangedFields`, optional `FilterFHIRPath`, optional search `Criteria` / `FilterParams` |
| `Channel` | `webhook` or `local` with `WebhookConfig` / `LocalConfig` |
| `DeliveryRecord` | Subscription ID, event sequence, attempt, status, response/error metadata |

Job type: `jobs.TypeSubscriptionsDeliver` (`subscriptions.deliver`).

## When to use it

- **Webhook notifications** when FHIR resources change (REST-hook style)
- **In-process reactions** — register a named local handler for create/update/delete
- **Filtered triggers** — e.g. `Observation.created` where `code = X` via FHIRPath
- **Field-scoped updates** — e.g. `Appointment.status` changed only
- **Edge (SQLite)** or **hub (Postgres)** — same package, different store wiring

## Usage

### 1. Register a subscription

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

Changed-field and FHIRPath examples:

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

### 2. Run the event processor

Set `Matcher.Registry` to the search parameter registry whenever subscriptions
use `FilterParams` / FHIR `Subscription.criteria`. A nil registry returns
`ErrNilRegistry` instead of silently skipping matches. `pkg/runtime` wires
`SubscriptionMatcher` with the FHIRPath engine and search registry, and
attaches that matcher to `SubscriptionProcessor`, so hosts do not have to.

```go
engine, _ := fhirpath.NewEngine(fhirpath.Config{})

processor := &subscriptions.Processor{
    Events:        db.OutboxStore(),      // or tdb.EventStore() on Postgres
    Cursors:       db.CursorStore(),
    Subscriptions: db.SubscriptionStore(),
    Jobs:          db.JobStore(),
    Resources:     db.ResourceStore(),
    History:       db.HistoryStore(),
    Matcher:       &subscriptions.Matcher{Engine: engine, Registry: searchRegistry},
    Scope:         "default",
}

// One batch or a loop
n, err := processor.RunOnce(ctx)
go processor.RunLoop(ctx, time.Second)
```

Checkpoint name: `subscriptions.CursorName(scope)` →
`subscriptions.processor.{scope}`.

### 3. Run delivery workers

```go
registry := subscriptions.NewHandlerRegistry()
registry.Register("on-patient-created", func(ctx context.Context, payload subscriptions.DeliverPayload, resourceJSON []byte, metadata map[string]any) error {
    // handle locally
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
go runner.RunOnce(ctx) // or wrap jobs.Runner in a loop
```

### 4. FHIR Subscription adapter (supported subset)

FHIR R4 `Subscription.criteria` is rest-hook only and fires on **create and
update** of matching resources (internal event `change`). Equality search
parameters work — for example `Patient?active=true` matches a Patient whose
`active` token is true. A default `pkg/runtime` wires `Matcher.Registry` and
`Matcher.Engine`; hosts that construct a matcher themselves must set both or
criteria matching returns `subscriptions.ErrNilRegistry`.

```go
rec, err := mgr.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
    Status:   "active",
    Criteria: "Patient?active=true",
    Channel: subscriptions.FHIRSubscriptionChannel{
        Type:     "rest-hook",
        Endpoint: "https://example.test/hook",
        Payload:  "application/fhir+json",
    },
}, extensions) // optional FHIRPath filter via app-controlled extension
```

Unsupported shapes return `subscriptions.ErrUnsupportedFHIR` — e.g.
`Patient?active:not=true`, `_include` / chained parameters, `websocket`
channel type.

Parse from FHIR JSON:

```go
input, extensions, err := subscriptions.ParseFHIRSubscriptionJSON(subscriptionJSON)
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

Delivery job IDs are deterministic:
`subscriptions:deliver:{subscriptionId}:{eventSequence}` — re-processing the
same event/subscription pair is idempotent.

## Where it fits

| Package | Role |
|---------|------|
| **store** | `EventStore`, `CursorStore`, `JobStore`, subscription stores |
| **jobs** | Durable delivery queue and retry runtime |
| **fhirpath** | In-resource filter evaluation (`FilterFHIRPath`) |
| **core** | Produces `ResourceEvent` entries (unchanged; subscriptions are optional downstream) |
| **sqlite** / **postgres** | Persistence backends |

See [doc.go](./doc.go) for the package entry point.
