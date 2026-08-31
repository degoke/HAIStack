package sync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestBuildHubWriteRejectsFHIRPayloadWithoutID(t *testing.T) {
	hub := &PostgresHub{}
	validator, err := hub.validator()
	if err != nil {
		t.Fatalf("validator: %v", err)
	}

	_, reject, ok := buildHubWrite(context.Background(), LocalEvent{
		EventID:      "event-1",
		ResourceType: "Patient",
		ResourceID:   "p1",
		Operation:    EventTypeResourceCreated,
		ResourceAfter: &types.ResourceEnvelope{
			ResourceType: "Patient",
			ID:           "p1",
			JSON:         []byte(`{"resourceType":"Patient","id":"p1","gender":42}`),
		},
	}, time.Now().UTC(), validator)
	if ok {
		t.Fatal("expected invalid FHIR payload to be rejected")
	}
	if !strings.Contains(reject.Reason, "gender") {
		t.Fatalf("reason = %q, want validation diagnostic", reject.Reason)
	}
}

func TestBuildHubWriteRejectsPayloadJSONIDMismatch(t *testing.T) {
	hub := &PostgresHub{}
	validator, err := hub.validator()
	if err != nil {
		t.Fatalf("validator: %v", err)
	}

	_, reject, ok := buildHubWrite(context.Background(), LocalEvent{
		EventID:      "event-2",
		ResourceType: "Patient",
		ResourceID:   "p1",
		Operation:    EventTypeResourceCreated,
		ResourceAfter: &types.ResourceEnvelope{
			ResourceType: "Patient",
			ID:           "p1",
			JSON:         []byte(`{"resourceType":"Patient","id":"p2"}`),
		},
	}, time.Now().UTC(), validator)
	if ok {
		t.Fatal("expected mismatched JSON identity to be rejected")
	}
	if !strings.Contains(reject.Reason, "JSON identity") {
		t.Fatalf("reason = %q, want JSON identity diagnostic", reject.Reason)
	}
}

func TestReplayedPushResultUsesAlreadyProcessedState(t *testing.T) {
	result := replayedPushResult("event-3", PushResult{
		EventID:            "event-3",
		State:              AckAccepted,
		CanonicalSequence:  42,
		CanonicalVersionID: "canonical-v1",
	})
	if result.State != AckAlreadyProcessed {
		t.Fatalf("state = %q, want already_processed", result.State)
	}
	if result.CanonicalSequence != 42 || result.CanonicalVersionID != "canonical-v1" {
		t.Fatalf("canonical metadata = %+v", result)
	}
}
