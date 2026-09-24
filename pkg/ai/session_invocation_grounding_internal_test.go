package ai

import (
	"testing"

	"github.com/degoke/haistack/pkg/store"
)

func TestInvocationToolGroundingContext_ReplaysToolEvidence(t *testing.T) {
	inv := "inv-1"
	events := []store.SessionEvent{
		{InvocationID: inv, Author: store.SessionAuthorUser, Content: "labs?"},
		{InvocationID: inv, Author: store.SessionAuthorModel, ToolCalls: []store.SessionToolCall{{
			ID: "t1", Name: ToolReadFhirResource, Arguments: `{"resourceType":"Patient","id":"p1"}`,
		}}},
		{InvocationID: inv, Author: store.SessionAuthorTool, ToolCallID: "t1", Content: `{"resourceType":"Observation","valueQuantity":{"value":99,"unit":"mg/dL"}}`},
		{InvocationID: inv, Author: store.SessionAuthorModel, Content: "Glucose was 999 mg/dL."},
	}
	summaries, inputs := invocationToolGroundingContext(events, inv, ToolContextJSON)
	if len(summaries) != 1 || len(inputs) != 1 {
		t.Fatalf("summaries=%d inputs=%d", len(summaries), len(inputs))
	}
	warnings := AnalyzeClinicalValueGrounding("Glucose was 999 mg/dL.", summaries)
	if len(warnings) == 0 {
		t.Fatal("expected clinical grounding warning on replay context")
	}
}
