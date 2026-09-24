package ai_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestExecutor_WriteAIAttribution(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true, enableAIAttribution: true})
	ctx := context.Background()
	res, err := h.exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName:       ai.ToolWriteFhirResource,
		Actor:          "agent-1",
		ConversationID: "conv-1",
		Input: map[string]any{
			"operation":    "create",
			"resourceType": "Patient",
			"fields": map[string]any{
				"name": []any{map[string]any{"family": "Lee"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data := dataMap(t, res.Data)
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("missing patient id")
	}
	patient, err := h.coreMem.Read(ctx, "Patient", id)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(patient.JSON, &root); err != nil {
		t.Fatal(err)
	}
	meta, _ := root["meta"].(map[string]any)
	if meta == nil {
		t.Fatal("expected meta")
	}
	security, _ := meta["security"].([]any)
	if !hasAIAST(security) {
		t.Fatalf("meta.security = %v", security)
	}
	if h.coreMem.countResourceType("Provenance") != 1 {
		t.Fatalf("provenance count = %d", h.coreMem.countResourceType("Provenance"))
	}
}

func hasAIAST(security []any) bool {
	for _, item := range security {
		coding, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if coding["system"] == ai.AIASTCodeSystem && coding["code"] == ai.AIASTCode {
			return true
		}
	}
	return false
}

func TestHarness_CommitWriteRequiresHostConfirm(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:                  h.exec,
		Model:                     &fakeChatModel{},
		Actor:                     "agent-1",
		RequireCommitConfirmation: true,
		CommitWriteConfirm: func(ctx context.Context, draft ai.ResourceWriteDraft) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.CommitWrite(context.Background(), ai.ResourceWriteDraft{
		Operation:    ai.WriteOperationCreate,
		ResourceType: "Patient",
		Fields:       map[string]any{"gender": "female"},
	})
	if err != nil {
		t.Fatalf("confirmed commit: %v", err)
	}

	harness2, _ := ai.NewHarness(ai.HarnessConfig{
		Executor:                  h.exec,
		Model:                     &fakeChatModel{},
		Actor:                     "agent-1",
		RequireCommitConfirmation: true,
	})
	_, err = harness2.CommitWrite(context.Background(), ai.ResourceWriteDraft{
		Operation:    ai.WriteOperationCreate,
		ResourceType: "Patient",
		Fields:       map[string]any{"gender": "male"},
	})
	if err != ai.ErrCommitNotConfirmed {
		t.Fatalf("err = %v", err)
	}
}
