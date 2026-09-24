package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestResourceWriteDraft_CreateAndUpdate(t *testing.T) {
	create := ai.ResourceWriteDraft{
		Operation:    ai.WriteOperationCreate,
		ResourceType: "Patient",
		Fields:       map[string]any{"gender": "female"},
	}
	tool, input, err := create.ToExecutorToolInput()
	if err != nil {
		t.Fatal(err)
	}
	if tool != ai.ToolCreateFhirResource {
		t.Fatalf("tool = %v", tool)
	}
	if input["fields"] == nil {
		t.Fatal("expected fields")
	}

	update := ai.ResourceWriteDraft{
		Operation:    ai.WriteOperationUpdate,
		ResourceType: "Patient",
		ID:           "p1",
		Patches:      map[string]any{"gender": "male"},
	}
	tool, input, err = update.ToExecutorToolInput()
	if err != nil {
		t.Fatal(err)
	}
	if tool != ai.ToolUpdateFhirResource || input["patches"] == nil {
		t.Fatalf("tool=%v input=%v", tool, input)
	}
}

func TestHarness_CommitWrite(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor: h.exec,
		Model:    &fakeChatModel{},
		Actor:    "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.CommitWrite(context.Background(), ai.ResourceWriteDraft{
		Operation:    ai.WriteOperationCreate,
		ResourceType: "Patient",
		Fields: map[string]any{
			"name": []any{map[string]any{"family": "Kim", "given": []string{"Bo"}}},
		},
	})
	if err != nil {
		t.Fatalf("CommitWrite: %v", err)
	}
}

func TestHarness_ProposeWriteDoesNotCommit(t *testing.T) {
	h := newTestHarness(t, harnessOptions{withCore: true, allowPatientWrite: true})
	before := len(h.resources.all())
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{ToolCalls: []ai.ChatToolCall{{
			ID:   "c1",
			Name: ai.ToolProposeWriteResource,
			Arguments: `{"operation":"create","resourceType":"Patient","fields":{"gender":"unknown"}}`,
		}}},
		{Content: "proposed"},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:                  h.exec,
		Model:                     model,
		Actor:                     "agent-1",
		EnableProposeWriteHelper: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "create patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.resources.all()) != before {
		t.Fatal("propose should not commit")
	}
}
