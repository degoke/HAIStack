// Package subscriptions implements haistack-subscriptions, the tenant-neutral
// event automation library for Health AI Stack.
//
// haistack-subscriptions consumes store.ResourceEvent entries produced by
// pkg/core writes and schedules durable notification delivery through pkg/jobs.
// It is a downstream consumer of the existing event log — subscriptions do not
// hook into pkg/core write paths and remain optional at deploy time.
//
// # Scope (v1)
//
// v1 supports:
//
//   - Internal trigger registration (resource type + create/update/delete event,
//     optional changed-field filters, optional FHIRPath predicates)
//   - A narrow adapter from a supported subset of FHIR Subscription resources
//   - Webhook (HTTP) and local (in-process handler) delivery channels
//   - Durable delivery logging and retries backed by pkg/jobs
//   - Postgres-first and SQLite edge persistence without Kafka/NATS
//
// v1 does not support:
//
//   - WebSocket, email, SMS, or message channel types
//   - Dead-letter queues or full delivery audit expansion
//   - Tenant semantics in the core API (Postgres scoping is via TenantDB wiring)
//   - Advanced FHIR Search criteria in Subscription.criteria (simple
//     ResourceType only, e.g. "Patient" not "Patient?active=true")
//
// # Public API
//
//   - Manager — register, update, disable, list, delete subscription records
//   - Processor — consume ResourceEvent entries and enqueue delivery jobs
//   - DeliveryWorker — execute queued deliveries
//   - DeliveryJobRunner — claim and run delivery jobs via pkg/jobs.Runner
//   - Matcher — evaluate triggers against current and previous resource state
//   - WebhookDispatcher — HTTP POST/PUT transport
//   - LocalDispatcher / HandlerRegistry — in-process handler transport
//   - RegisterFromFHIRSubscription — FHIR Subscription adapter (supported subset)
//   - CursorName — processor checkpoint naming helper
//
// Core types: SubscriptionRecord, Trigger, Channel, RetryPolicy, DeliveryRecord,
// DeliverPayload.
//
// Job type: jobs.TypeSubscriptionsDeliver ("subscriptions.deliver").
//
// # Trigger examples
//
//	Patient.created:
//	    Trigger{ResourceType: "Patient", Event: TriggerEventCreate}
//
//	Appointment.status changed:
//	    Trigger{ResourceType: "Appointment", Event: TriggerEventUpdate,
//	            ChangedFields: []string{"status"}}
//
//	Observation.created where code = X:
//	    Trigger{ResourceType: "Observation", Event: TriggerEventCreate,
//	            FilterFHIRPath: "code.coding.code = '8867-4'"}
//
// FilterFHIRPath is evaluated with pkg/fhirpath against the current resource on
// create/update. Changed-field matching compares top-level JSON fields between
// the previous history snapshot and the current resource.
//
// # Store contracts
//
// Subscription-specific persistence lives in pkg/store:
//
//   - store.SubscriptionStore — registry CRUD (subscription_registry)
//   - store.SubscriptionDeliveryStore — delivery attempt log
//     (subscription_delivery_log)
//
// Reused contracts:
//
//   - store.EventStore — change event source (sync_outbox on SQLite, event_log on
//     Postgres)
//   - store.CursorStore — processor checkpoint position
//   - store.JobStore — durable delivery work and retries (no dedicated retry table)
//   - store.ResourceStore / store.HistoryStore — resource context for matching
//
// Postgres: pkg/postgres.TenantDB.SubscriptionStore(),
// SubscriptionDeliveryStore(). SQLite: pkg/sqlite.DB.SubscriptionStore(),
// SubscriptionDeliveryStore().
//
// Local handler names are registered in-memory via HandlerRegistry; they are not
// persisted.
//
// # Typical wiring
//
//	mgr := &subscriptions.Manager{Store: db.SubscriptionStore()}
//	_, _ = mgr.Register(ctx, "patient-created", trigger, channel, retryPolicy)
//
//	engine, _ := fhirpath.NewEngine(fhirpath.Config{})
//	processor := &subscriptions.Processor{
//	    Events:        db.OutboxStore(),
//	    Cursors:       db.CursorStore(),
//	    Subscriptions: db.SubscriptionStore(),
//	    Jobs:          db.JobStore(),
//	    Resources:     db.ResourceStore(),
//	    History:       db.HistoryStore(),
//	    Matcher:       &subscriptions.Matcher{Engine: engine},
//	}
//	_, _ = processor.RunOnce(ctx)
//
//	registry := subscriptions.NewHandlerRegistry()
//	registry.Register("on-patient-created", handler)
//	worker := &subscriptions.DeliveryWorker{
//	    Subscriptions: db.SubscriptionStore(),
//	    Deliveries:    db.SubscriptionDeliveryStore(),
//	    Resources:     db.ResourceStore(),
//	    Webhook:       &subscriptions.WebhookDispatcher{},
//	    Local:         &subscriptions.LocalDispatcher{Registry: registry},
//	}
//	runner := &subscriptions.DeliveryJobRunner{Jobs: db.JobStore(), Worker: worker}
//	_, _ = runner.RunOnce(ctx)
//
// # Runtime flow
//
//  1. Processor.RunOnce/RunLoop reads EventStore.ReadSince using CursorStore.
//  2. For each event, active subscriptions are loaded by resource type and event
//     kind.
//  3. Matcher evaluates changed fields (via history) and FHIRPath filters.
//  4. On match, one delivery job is enqueued per subscription-event pair.
//  5. DeliveryWorker resolves subscription context, dispatches via webhook or
//     local handler, and appends/updates delivery-log records.
//  6. Retry policy uses pkg/jobs backoff; terminal failure remains in job state
//     plus delivery log (no DLQ table in v1).
//
// Delivery job IDs are deterministic (subscriptions:deliver:{id}:{sequence}) so
// the same event/subscription pair is idempotent across processor restarts.
//
// # Ownership boundaries
//
// haistack-subscriptions owns trigger matching, delivery scheduling, transport
// adapters, FHIR Subscription adaptation, and operational delivery logging.
// It does not own resource writes, event emission, job persistence, or
// compliance-grade audit (pkg/audit may mirror lifecycle events later without
// changing delivery semantics).
//
// # File layout
//
//   - doc.go — package documentation
//   - types.go — SubscriptionRecord, Trigger, Channel, DeliveryRecord, payloads
//   - errors.go — package sentinels
//   - codec.go — store record conversion and validation helpers
//   - manager.go — subscription registry CRUD
//   - matcher.go — event predicate evaluation
//   - processor.go — event intake and job enqueue
//   - delivery_worker.go — delivery execution and DeliveryJobRunner
//   - webhook_dispatcher.go — HTTP webhook transport
//   - local_dispatcher.go — in-process handler transport
//   - handler_registry.go — in-memory local handler registry
//   - fhir_adapter.go — FHIR Subscription subset adapter
//
// See README.md for extended usage and integration notes.
package subscriptions
