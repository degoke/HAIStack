package runtime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/subscriptions"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestSQLiteRuntimeWiresSubscriptionMatcherRegistry(t *testing.T) {
	ctx := t.Context()
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/subs.db").
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	svc := rt.Services()
	if svc.SubscriptionMatcher == nil {
		t.Fatal("expected SubscriptionMatcher")
	}
	if svc.SubscriptionMatcher.Engine == nil {
		t.Fatal("expected Matcher.Engine")
	}
	if svc.SubscriptionMatcher.Registry == nil {
		t.Fatal("expected Matcher.Registry")
	}
	if svc.SubscriptionProcessor == nil {
		t.Fatal("expected SubscriptionProcessor")
	}
	if svc.SubscriptionProcessor.Matcher != svc.SubscriptionMatcher {
		t.Fatal("processor matcher should be the wired matcher")
	}
	if svc.SubscriptionManager == nil {
		t.Fatal("expected SubscriptionManager")
	}

	rec, err := svc.SubscriptionManager.RegisterFromFHIRSubscription(ctx, subscriptions.FHIRSubscriptionInput{
		Status:   "active",
		Criteria: "Patient?active=true",
		Channel: subscriptions.FHIRSubscriptionChannel{
			Type:     "rest-hook",
			Endpoint: "https://example.test/hook",
		},
	}, nil)
	if err != nil {
		t.Fatalf("RegisterFromFHIRSubscription: %v", err)
	}

	_, err = svc.SubscriptionMatcher.Matches(ctx, rec.Trigger, subscriptions.MatchContext{
		Event:   store.ResourceEvent{ResourceType: "Patient", Action: store.EventActionCreate},
		Current: patientActiveEnvelope(t, "p1", true),
	})
	if errors.Is(err, subscriptions.ErrNilRegistry) {
		t.Fatal("wired matcher returned ErrNilRegistry for Patient?active=true")
	}
	if err != nil {
		t.Fatalf("Matches: %v", err)
	}
}

func patientActiveEnvelope(t *testing.T, id string, active bool) *types.ResourceEnvelope {
	t.Helper()
	payload := map[string]any{
		"resourceType": "Patient",
		"id":           id,
		"active":       active,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &types.ResourceEnvelope{ResourceType: "Patient", ID: id, JSON: data}
}
