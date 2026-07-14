package subscriptions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/subscriptions"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func openTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("fhirpath engine: %v", err)
	}
	return engine
}

func envelope(t *testing.T, resourceType, id string, fields map[string]any) *types.ResourceEnvelope {
	t.Helper()
	fields["resourceType"] = resourceType
	fields["id"] = id
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &types.ResourceEnvelope{ResourceType: resourceType, ID: id, JSON: data}
}

func TestManagerRegisterListGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	mgr := &subscriptions.Manager{Store: db.SubscriptionStore()}

	rec, err := mgr.Register(ctx, "patient-create", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type: subscriptions.ChannelTypeLocal,
		Local: &subscriptions.LocalConfig{HandlerName: "demo"},
	}, subscriptions.RetryPolicy{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if rec.ID == "" || rec.Status != store.SubscriptionStatusActive {
		t.Fatalf("unexpected record: %#v", rec)
	}

	got, err := mgr.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "patient-create" {
		t.Fatalf("name = %q", got.Name)
	}

	list, err := mgr.List(ctx, store.SubscriptionStatusActive, "Patient", subscriptions.TriggerEventCreate)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %#v err=%v", list, err)
	}

	got.Name = "renamed"
	updated, err := mgr.Update(ctx, got)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("updated name = %q", updated.Name)
	}

	if err := mgr.Disable(ctx, rec.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	disabled, err := mgr.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get disabled: %v", err)
	}
	if disabled.Status != store.SubscriptionStatusDisabled {
		t.Fatalf("status = %q", disabled.Status)
	}

	if err := mgr.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := mgr.Get(ctx, rec.ID); err == nil {
		t.Fatal("expected get after delete to fail")
	}
}

func TestMatcherCreateUpdateDeleteAndFHIRPath(t *testing.T) {
	ctx := context.Background()
	matcher := &subscriptions.Matcher{Engine: mustEngine(t)}

	patient := envelope(t, "Patient", "p1", map[string]any{"active": true})
	ok, err := matcher.Matches(ctx, subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.MatchContext{
		Event:   store.ResourceEvent{ResourceType: "Patient", Action: store.EventActionCreate},
		Current: patient,
	})
	if err != nil || !ok {
		t.Fatalf("create match = %v err=%v", ok, err)
	}

	obsMatch := envelope(t, "Observation", "o1", map[string]any{
		"status": "final",
		"code": map[string]any{
			"coding": []any{map[string]any{"code": "8867-4"}},
		},
	})
	ok, err = matcher.Matches(ctx, subscriptions.Trigger{
		ResourceType:   "Observation",
		Event:          subscriptions.TriggerEventCreate,
		FilterFHIRPath: "code.coding.code = '8867-4'",
	}, subscriptions.MatchContext{
		Event:   store.ResourceEvent{ResourceType: "Observation", Action: store.EventActionCreate},
		Current: obsMatch,
	})
	if err != nil || !ok {
		t.Fatalf("fhirpath match = %v err=%v", ok, err)
	}

	obsOther := envelope(t, "Observation", "o2", map[string]any{
		"status": "final",
		"code": map[string]any{
			"coding": []any{map[string]any{"code": "other"}},
		},
	})
	ok, err = matcher.Matches(ctx, subscriptions.Trigger{
		ResourceType:   "Observation",
		Event:          subscriptions.TriggerEventCreate,
		FilterFHIRPath: "code.coding.code = '8867-4'",
	}, subscriptions.MatchContext{
		Event:   store.ResourceEvent{ResourceType: "Observation", Action: store.EventActionCreate},
		Current: obsOther,
	})
	if err != nil || ok {
		t.Fatalf("non-match = %v err=%v", ok, err)
	}

	prevAppt := envelope(t, "Appointment", "a1", map[string]any{"status": "booked"})
	currAppt := envelope(t, "Appointment", "a1", map[string]any{"status": "arrived"})
	ok, err = matcher.Matches(ctx, subscriptions.Trigger{
		ResourceType:  "Appointment",
		Event:         subscriptions.TriggerEventUpdate,
		ChangedFields: []string{"status"},
	}, subscriptions.MatchContext{
		Event:    store.ResourceEvent{ResourceType: "Appointment", Action: store.EventActionUpdate},
		Previous: prevAppt,
		Current:  currAppt,
	})
	if err != nil || !ok {
		t.Fatalf("status change match = %v err=%v", ok, err)
	}

	currNote := envelope(t, "Appointment", "a1", map[string]any{"status": "arrived", "comment": "x"})
	ok, err = matcher.Matches(ctx, subscriptions.Trigger{
		ResourceType:  "Appointment",
		Event:         subscriptions.TriggerEventUpdate,
		ChangedFields: []string{"status"},
	}, subscriptions.MatchContext{
		Event:    store.ResourceEvent{ResourceType: "Appointment", Action: store.EventActionUpdate},
		Previous: currAppt,
		Current:  currNote,
	})
	if err != nil || ok {
		t.Fatalf("unrelated update should not match: ok=%v err=%v", ok, err)
	}
}

func TestProcessorEnqueuesDeliveryAndResumesCursor(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return fixed }}
	_, err := mgr.Register(ctx, "patient-create", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type:    subscriptions.ChannelTypeWebhook,
		Webhook: &subscriptions.WebhookConfig{URL: "http://example.test/hook"},
	}, subscriptions.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	events := db.OutboxStore()
	ev, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "1",
		Action:       store.EventActionCreate,
		Timestamp:    fixed,
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	processor := &subscriptions.Processor{
		Events:        events,
		Cursors:       db.CursorStore(),
		Subscriptions: db.SubscriptionStore(),
		Jobs:          db.JobStore(),
		Matcher:       &subscriptions.Matcher{Engine: mustEngine(t)},
		Now:           func() time.Time { return fixed },
	}
	n, err := processor.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if n != 1 {
		t.Fatalf("processed = %d", n)
	}
	subs, err := mgr.List(ctx, store.SubscriptionStatusActive, "Patient", subscriptions.TriggerEventCreate)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subs = %#v err=%v", subs, err)
	}
	job, err := db.JobStore().Get(ctx, subscriptions.DeliveryJobID(subs[0].ID, ev.Sequence))
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Type != jobs.TypeSubscriptionsDeliver {
		t.Fatalf("job type = %q", job.Type)
	}

	n, err = processor.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no new events, got %d", n)
	}
}

func TestProcessorMatchesAgainstEventVersionNotLatestResource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return fixed }}
	_, err := mgr.Register(ctx, "obs-create", subscriptions.Trigger{
		ResourceType:   "Observation",
		Event:          subscriptions.TriggerEventCreate,
		FilterFHIRPath: "code.coding.code = '8867-4'",
	}, subscriptions.Channel{
		Type:  subscriptions.ChannelTypeLocal,
		Local: &subscriptions.LocalConfig{HandlerName: "demo"},
	}, subscriptions.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	history := db.HistoryStore()
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: "Observation",
		ID:           "o1",
		VersionID:    "v1",
		Action:       store.VersionActionCreate,
		Timestamp:    fixed,
		Resource: envelope(t, "Observation", "o1", map[string]any{
			"status": "final",
			"code": map[string]any{
				"coding": []any{map[string]any{"code": "8867-4"}},
			},
		}),
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	// Simulate the resource having changed before the processor consumes the event.
	if err := db.ResourceStore().Create(ctx, envelope(t, "Observation", "o1", map[string]any{
		"status": "final",
		"code": map[string]any{
			"coding": []any{map[string]any{"code": "other"}},
		},
	})); err != nil {
		t.Fatalf("create current resource: %v", err)
	}
	ev, err := db.OutboxStore().Append(ctx, store.ResourceEvent{
		ResourceType: "Observation",
		ID:           "o1",
		VersionID:    "v1",
		Action:       store.EventActionCreate,
		Timestamp:    fixed,
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	processor := &subscriptions.Processor{
		Events:        db.OutboxStore(),
		Cursors:       db.CursorStore(),
		Subscriptions: db.SubscriptionStore(),
		Jobs:          db.JobStore(),
		History:       history,
		Resources:     db.ResourceStore(),
		Matcher:       &subscriptions.Matcher{Engine: mustEngine(t)},
	}
	if _, err := processor.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}
	subs, err := mgr.List(ctx, store.SubscriptionStatusActive, "Observation", subscriptions.TriggerEventCreate)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subs = %#v err=%v", subs, err)
	}
	job, err := db.JobStore().Get(ctx, subscriptions.DeliveryJobID(subs[0].ID, ev.Sequence))
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil {
		t.Fatal("expected delivery job to be enqueued from historical event version")
	}
}

func TestWebhookDeliverySuccessAndRetry(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := now

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return clock }}
	rec, err := mgr.Register(ctx, "hook", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type: subscriptions.ChannelTypeWebhook,
		Webhook: &subscriptions.WebhookConfig{
			URL:    server.URL,
			Method: http.MethodPost,
		},
	}, subscriptions.RetryPolicy{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	resources := db.ResourceStore()
	patient := envelope(t, "Patient", "p1", map[string]any{"active": true})
	if err := resources.Create(ctx, patient); err != nil {
		t.Fatalf("create patient: %v", err)
	}

	payload := subscriptions.DeliverPayload{
		SubscriptionID: rec.ID,
		EventSequence:  1,
		ResourceType:   "Patient",
		ResourceID:     "p1",
		VersionID:      "1",
		Action:         store.EventActionCreate,
	}
	jobStore := db.JobStore()
	job, err := jobs.Enqueue(ctx, jobStore, jobs.TypeSubscriptionsDeliver, payload, jobs.EnqueueOptions{
		ID:  "delivery-job-1",
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	worker := &subscriptions.DeliveryWorker{
		Subscriptions: db.SubscriptionStore(),
		Deliveries:    db.SubscriptionDeliveryStore(),
		Resources:     resources,
		Webhook:       &subscriptions.WebhookDispatcher{Client: server.Client()},
		Now:           func() time.Time { return clock },
	}
	runner := &subscriptions.DeliveryJobRunner{
		Jobs:        jobStore,
		Worker:      worker,
		MaxAttempts: 2,
		Now:         func() time.Time { return clock },
	}
	processed, err := runner.RunOnce(ctx)
	if err == nil {
		t.Fatalf("expected first attempt failure")
	}
	if !processed {
		t.Fatal("expected job processed")
	}

	updated, err := jobStore.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updated.Status != store.JobStatusPending {
		t.Fatalf("after retryable failure status = %q", updated.Status)
	}

	clock = clock.Add(2 * time.Minute)
	processed, err = runner.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if !processed {
		t.Fatal("expected second attempt processed")
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d", hits.Load())
	}
	logs, err := db.SubscriptionDeliveryStore().List(ctx, store.DeliveryListQuery{
		SubscriptionID: rec.ID,
		EventSequence:  1,
	})
	if err != nil || len(logs) == 0 {
		t.Fatalf("delivery logs = %#v err=%v", logs, err)
	}
}

func TestLocalHandlerDispatch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	registry := subscriptions.NewHandlerRegistry()
	var called bool
	registry.Register("demo", func(ctx context.Context, payload subscriptions.DeliverPayload, resourceJSON []byte, metadata map[string]any) error {
		called = true
		return nil
	})

	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return fixed }}
	rec, err := mgr.Register(ctx, "local", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type:  subscriptions.ChannelTypeLocal,
		Local: &subscriptions.LocalConfig{HandlerName: "demo"},
	}, subscriptions.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	worker := &subscriptions.DeliveryWorker{
		Subscriptions: db.SubscriptionStore(),
		Deliveries:    db.SubscriptionDeliveryStore(),
		Local:         &subscriptions.LocalDispatcher{Registry: registry},
		Now:           func() time.Time { return fixed },
	}
	job, err := jobs.NewJob(jobs.TypeSubscriptionsDeliver, subscriptions.DeliverPayload{
		SubscriptionID: rec.ID,
		EventSequence:  9,
		ResourceType:   "Patient",
		ResourceID:     "p1",
		Action:         store.EventActionCreate,
	}, jobs.EnqueueOptions{ID: "local-job", Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	if err := worker.HandleJob(ctx, job); err != nil {
		t.Fatalf("handle job: %v", err)
	}
	if !called {
		t.Fatal("expected local handler to be called")
	}
}

func TestDeliveryWorkerUsesEventVersionResource(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	registry := subscriptions.NewHandlerRegistry()
	var gotJSON []byte
	registry.Register("demo", func(ctx context.Context, payload subscriptions.DeliverPayload, resourceJSON []byte, metadata map[string]any) error {
		gotJSON = append([]byte(nil), resourceJSON...)
		return nil
	})

	if err := db.HistoryStore().AppendVersion(ctx, store.ResourceVersion{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "v1",
		Action:       store.VersionActionCreate,
		Timestamp:    fixed,
		Resource:     envelope(t, "Patient", "p1", map[string]any{"active": true}),
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	if err := db.ResourceStore().Create(ctx, envelope(t, "Patient", "p1", map[string]any{"active": false})); err != nil {
		t.Fatalf("create current resource: %v", err)
	}

	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return fixed }}
	rec, err := mgr.Register(ctx, "local", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type:  subscriptions.ChannelTypeLocal,
		Local: &subscriptions.LocalConfig{HandlerName: "demo"},
	}, subscriptions.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	worker := &subscriptions.DeliveryWorker{
		Subscriptions: db.SubscriptionStore(),
		Deliveries:    db.SubscriptionDeliveryStore(),
		Resources:     db.ResourceStore(),
		History:       db.HistoryStore(),
		Local:         &subscriptions.LocalDispatcher{Registry: registry},
		Now:           func() time.Time { return fixed },
	}
	job, err := jobs.NewJob(jobs.TypeSubscriptionsDeliver, subscriptions.DeliverPayload{
		SubscriptionID: rec.ID,
		EventSequence:  9,
		ResourceType:   "Patient",
		ResourceID:     "p1",
		VersionID:      "v1",
		Action:         store.EventActionCreate,
	}, jobs.EnqueueOptions{ID: "local-job-versioned", Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	if err := worker.HandleJob(ctx, job); err != nil {
		t.Fatalf("handle job: %v", err)
	}
	if string(gotJSON) == "" {
		t.Fatal("expected local handler to receive resource json")
	}
	if string(gotJSON) == string(envelope(t, "Patient", "p1", map[string]any{"active": false}).JSON) {
		t.Fatal("expected delivery worker to use historical event version, not latest current state")
	}
}

func TestRegisterFromFHIRSubscriptionSupportedAndUnsupported(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	mgr := &subscriptions.Manager{Store: db.SubscriptionStore()}

	rec, err := mgr.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
		Status:   "active",
		Criteria: "Patient",
		Channel: subscriptions.FHIRSubscriptionChannel{
			Type:     "rest-hook",
			Endpoint: "https://example.test/hook",
			Payload:  "application/fhir+json",
		},
	}, nil)
	if err != nil {
		t.Fatalf("register from fhir: %v", err)
	}
	if rec.Trigger.ResourceType != "Patient" || rec.Channel.Type != subscriptions.ChannelTypeWebhook {
		t.Fatalf("record = %#v", rec)
	}

	_, err = mgr.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
		Status:   "active",
		Criteria: "Patient?active=true",
		Channel: subscriptions.FHIRSubscriptionChannel{
			Type:     "rest-hook",
			Endpoint: "https://example.test/hook",
		},
	}, nil)
	if err == nil {
		t.Fatal("expected unsupported criteria error")
	}

	_, err = mgr.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
		Status:   "active",
		Criteria: "Patient",
		Channel: subscriptions.FHIRSubscriptionChannel{
			Type: "websocket",
		},
	}, nil)
	if err == nil {
		t.Fatal("expected unsupported channel error")
	}
}

func TestInactiveSubscriptionIgnoredByProcessor(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	mgr := &subscriptions.Manager{Store: db.SubscriptionStore(), Now: func() time.Time { return fixed }}
	rec, err := mgr.Register(ctx, "disabled", subscriptions.Trigger{
		ResourceType: "Patient",
		Event:        subscriptions.TriggerEventCreate,
	}, subscriptions.Channel{
		Type:    subscriptions.ChannelTypeWebhook,
		Webhook: &subscriptions.WebhookConfig{URL: "http://example.test"},
	}, subscriptions.RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mgr.Disable(ctx, rec.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	events := db.OutboxStore()
	if _, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "p1",
		Action:       store.EventActionCreate,
		Timestamp:    fixed,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	processor := &subscriptions.Processor{
		Events:        events,
		Cursors:       db.CursorStore(),
		Subscriptions: db.SubscriptionStore(),
		Jobs:          db.JobStore(),
		Matcher:       &subscriptions.Matcher{Engine: mustEngine(t)},
		Now:           func() time.Time { return fixed },
	}
	if _, err := processor.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}
	claimed, err := db.JobStore().ClaimNext(ctx, jobs.TypeSubscriptionsDeliver)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed != nil {
		t.Fatalf("expected no delivery job, got %#v", claimed)
	}
}
