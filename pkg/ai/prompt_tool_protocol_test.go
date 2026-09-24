package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestParsePromptToolCalls_InlineJSON(t *testing.T) {
	allowed := ai.ChatToolsFromDescriptors(ai.GenericToolDescriptors())
	calls, err := ai.ParsePromptToolCalls(
		`I will read the patient.
{"tool":"read_fhir_resource","input":{"resourceType":"Patient","id":"pat-jane"}}`,
		allowed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != ai.ToolReadFhirResource {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestParsePromptToolCalls_FencedJSON(t *testing.T) {
	allowed := ai.ChatToolsFromDescriptors(ai.GenericToolDescriptors())
	calls, err := ai.ParsePromptToolCalls("```json\n{\"toolName\":\"read_fhir_resource\",\"arguments\":{\"resourceType\":\"Patient\",\"id\":\"x\"}}\n```", allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestParsePromptToolCalls_RejectsUnknownTool(t *testing.T) {
	_, err := ai.ParsePromptToolCalls(`{"tool":"unknown_tool","input":{}}`, ai.ChatToolsFromDescriptors(ai.GenericToolDescriptors()))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHarness_PromptJSONToolLoop(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	model := &recordingChatModel{responses: []*ai.ChatResponse{
		{Content: `{"tool":"read_fhir_resource","input":{"resourceType":"Patient","id":"pat-jane"}}`},
		{Content: "Jane is in the chart."},
	}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:         h.exec,
		Model:            model,
		Actor:            "agent-1",
		ToolCallProtocol: ai.ToolCallProtocolPromptJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := harness.Chat(context.Background(), "load jane")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer, "chart") {
		t.Fatalf("answer = %q", res.Answer)
	}
	if len(res.ToolResults) != 1 {
		t.Fatalf("tool results = %d", len(res.ToolResults))
	}
}
