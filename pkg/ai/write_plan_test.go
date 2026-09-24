package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestResourceWritePlan_ToTransactionInput(t *testing.T) {
	plan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{
			{
				Operation:    ai.WriteOperationCreate,
				ResourceType: "Observation",
				Fields:       map[string]any{"status": "final"},
			},
			{
				Operation:    ai.WriteOperationUpdate,
				ResourceType: "Patient",
				ID:           "p1",
				Patches:      map[string]any{"name[0].family": "X"},
			},
		},
	}
	input, err := plan.ToTransactionInput()
	if err != nil {
		t.Fatal(err)
	}
	if input["bundleType"] != "transaction" {
		t.Fatalf("bundleType = %v", input["bundleType"])
	}
	entries, ok := input["entries"].([]map[string]any)
	if !ok {
		// JSON round-trip types may be []any from map construction
		raw, _ := input["entries"].([]any)
		if len(raw) != 2 {
			t.Fatalf("entries = %#v", input["entries"])
		}
	} else if len(entries) != 2 {
		t.Fatalf("len = %d", len(entries))
	}
}

func TestResourceWritePlan_RejectsProvenance(t *testing.T) {
	plan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{{
			Operation:    ai.WriteOperationCreate,
			ResourceType: "Provenance",
			Fields:       map[string]any{"target": []any{}},
		}},
	}
	if err := plan.Validate(); err == nil {
		t.Fatal("expected error for model Provenance")
	}
}

func TestHarness_CommitWritePlan(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true, seedPatients: true})
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    &fakeChatModel{},
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.CommitWritePlan(context.Background(), ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{
			{
				Operation:    ai.WriteOperationUpdate,
				ResourceType: "Patient",
				ID:           "pat-jane",
				Patches:      map[string]any{"name[0].family": "Plan"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitWritePlan: %v", err)
	}
}
