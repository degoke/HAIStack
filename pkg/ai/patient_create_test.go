package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestPatientCreateDraft_ToWriteInput(t *testing.T) {
	draft := ai.PatientCreateDraft{
		Family: "Smith",
		Given:  []string{"Alex"},
		Gender: "male",
		Phone:  "555-0199",
	}
	input, err := draft.ToWriteFhirResourceInput()
	if err != nil {
		t.Fatal(err)
	}
	if input["operation"] != ai.WriteOperationCreate {
		t.Fatalf("operation = %v", input["operation"])
	}
	fields, _ := input["fields"].(map[string]any)
	if fields == nil {
		t.Fatal("missing fields")
	}
	telecom, _ := fields["telecom"].([]any)
	if len(telecom) != 1 {
		t.Fatalf("telecom = %v", fields["telecom"])
	}
}

func TestExtractPatientCreateDraft(t *testing.T) {
	msgs := []ai.ChatMessage{{
		Role: ai.ChatRoleAssistant,
		Content: "Here is the draft:\n```patient_create\n{\"family\":\"Lee\",\"given\":[\"Sam\"]}\n```",
	}}
	draft, ok := ai.ExtractPatientCreateDraft(msgs)
	if !ok {
		t.Fatal("expected draft")
	}
	if draft.Family != "Lee" {
		t.Fatalf("family = %q", draft.Family)
	}
}

func TestHarness_CommitPatientCreate(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    &fakeChatModel{},
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := harness.CommitPatientCreate(context.Background(), ai.PatientCreateDraft{
		Family: "Nguyen",
		Given:  []string{"Minh"},
	})
	if err != nil {
		t.Fatalf("CommitPatientCreate: %v", err)
	}
	if res.ToolName != ai.ToolCreateFhirResource {
		t.Fatalf("tool = %q", res.ToolName)
	}
}

type fakeChatModel struct{}

func (f *fakeChatModel) Name() string { return "fake" }

func (f *fakeChatModel) Chat(_ context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	return &ai.ChatResponse{Adapter: "fake", Content: "ok"}, nil
}
