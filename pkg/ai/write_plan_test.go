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

func TestResourceWritePlan_ReadOnlyCannotCommit(t *testing.T) {
	plan := ai.ResourceWritePlan{
		BundleType: "batch",
		Entries: []ai.ResourceWriteDraft{
			{Operation: ai.WriteOperationRead, ResourceType: "Patient", ID: "p1"},
		},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := plan.ToTransactionInput(); err == nil {
		t.Fatal("expected commit error for read-only plan")
	}
}

func TestResourceWritePlan_BatchReadAndUpdate(t *testing.T) {
	plan := ai.ResourceWritePlan{
		BundleType: "batch",
		Entries: []ai.ResourceWriteDraft{
			{Operation: ai.WriteOperationRead, ResourceType: "Patient", ID: "p1"},
			{
				Operation:    ai.WriteOperationUpdate,
				ResourceType: "Patient",
				ID:           "p1",
				Patches:      map[string]any{"gender": "male"},
			},
		},
	}
	input, err := plan.ToTransactionInput()
	if err != nil {
		t.Fatal(err)
	}
	if input["bundleType"] != "batch" {
		t.Fatalf("bundleType = %v", input["bundleType"])
	}
	roundTrip, err := ai.ResourceWritePlanFromTransactionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Entries) != 2 || roundTrip.Entries[0].Operation != ai.WriteOperationRead {
		t.Fatalf("round trip = %#v", roundTrip.Entries)
	}
}

func TestHarness_CommitWritePlan_PolicyApproval(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:              true,
		allowPatientWrite:     true,
		writeRequiresApproval: true,
		seedPatients:          true,
	})
	store := ai.NewMemoryApprovalStore()
	exec, err := ai.NewExecutor(ai.Config{
		Core:          h.core,
		Policy:        h.policy,
		Audit:         h.audit,
		ApprovalStore: store,
		Now:           h.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: exec,
		Model:    &fakeChatModel{},
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{
			{
				Operation:    ai.WriteOperationCreate,
				ResourceType: "Patient",
				Fields:       map[string]any{"gender": "unknown"},
			},
		},
	}
	pending, err := harness.CommitWritePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("CommitWritePlan: %v", err)
	}
	if pending == nil || !pending.ApprovalRequired || pending.ApprovalToken == "" {
		t.Fatalf("expected approval-required, got %#v", pending)
	}
	if err := store.Approve(pending.ApprovalToken); err != nil {
		t.Fatal(err)
	}
	committed, err := harness.CommitWritePlanWithOptions(context.Background(), plan, ai.CommitWriteOptions{
		ApprovalToken: pending.ApprovalToken,
	})
	if err != nil {
		t.Fatalf("approved commit: %v", err)
	}
	if committed.ApprovalRequired {
		t.Fatal("write still pending after approval")
	}
}

func TestHarness_CommitWritePlan_UpdateApproval(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		withCore:          true,
		allowPatientWrite: true,
		seedPatients:      true,
	})
	h.policy.Write["Patient"] = ai.WriteTypePolicy{
		CreateFields:   []string{"name", "gender"},
		UpdateFields:   []string{"name[0].family", "gender"},
		UpdateApproval: true,
	}
	store := ai.NewMemoryApprovalStore()
	exec, err := ai.NewExecutor(ai.Config{
		Core:          h.core,
		Policy:        h.policy,
		Audit:         h.audit,
		ApprovalStore: store,
		Now:           h.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: exec,
		Model:    &fakeChatModel{},
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := ai.ResourceWritePlan{
		Entries: []ai.ResourceWriteDraft{
			{
				Operation:    ai.WriteOperationUpdate,
				ResourceType: "Patient",
				ID:           "pat-jane",
				Patches:      map[string]any{"gender": "male"},
			},
		},
	}
	pending, err := harness.CommitWritePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || !pending.ApprovalRequired {
		t.Fatalf("expected update approval-required, got %#v", pending)
	}
	if err := store.Approve(pending.ApprovalToken); err != nil {
		t.Fatal(err)
	}
	_, err = harness.CommitWritePlanWithOptions(context.Background(), plan, ai.CommitWriteOptions{
		ApprovalToken: pending.ApprovalToken,
	})
	if err != nil {
		t.Fatal(err)
	}
}
