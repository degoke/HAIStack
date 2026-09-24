package ai_test

import (
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestHarnessExecutorGuardrails(t *testing.T) {
	notes := ai.HarnessExecutorGuardrails(ai.Config{Policy: ai.NewAllowListPolicy()})
	if len(notes) == 0 {
		t.Fatal("expected guardrail notes")
	}
	text := ai.FormatHarnessExecutorGuardrails(ai.Config{Policy: ai.NewAllowListPolicy()})
	if !strings.Contains(text, "AuditRequired") {
		t.Fatalf("missing audit note: %s", text)
	}
}

func TestFilterToolDescriptorsForHarness(t *testing.T) {
	filtered := ai.FilterToolDescriptorsForHarness(ai.GenericToolDescriptors(), true)
	for _, d := range filtered {
		if ai.IsWriteTool(d.Name) {
			t.Fatalf("write tool %q should be filtered", d.Name)
		}
	}
}

func TestMergeHarnessCitations(t *testing.T) {
	merged := ai.MergeHarnessCitations(nil, []ai.HarnessToolResult{{
		Result: &ai.ToolResult{
			Citations: []ai.Citation{{Kind: "resource", Ref: "Patient/p1"}},
		},
	}})
	if len(merged) != 1 || merged[0].Ref != "Patient/p1" {
		t.Fatalf("citations = %v", merged)
	}
}
