package main

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/audit"
)

func TestAIPipelineProvenanceChain(t *testing.T) {
	bundle, err := Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Track != "E" {
		t.Fatalf("track = %q", bundle.Track)
	}
	if !bundle.FAIR.Synthetic || bundle.FAIR.PHI {
		t.Fatal("dataset must be synthetic and contain no PHI")
	}
	if len(bundle.Inputs) != len(pipelinePatients())+len(pipelineObservations()) {
		t.Fatalf("inputs = %d", len(bundle.Inputs))
	}
	for _, in := range bundle.Inputs {
		if in.Hash == "" || !in.Validated {
			t.Fatalf("input %+v missing hash or validation", in)
		}
	}
	if bundle.View.Name != viewName || bundle.View.Version != viewVersion {
		t.Fatalf("view = %+v", bundle.View)
	}
	if bundle.View.RowCount != 5 {
		t.Fatalf("row count = %d, want 5 (preliminary filtered)", bundle.View.RowCount)
	}
	if bundle.View.Definition == "" || bundle.View.RowHash == "" {
		t.Fatal("view hashes missing")
	}
	if bundle.Policy.Hash == "" {
		t.Fatal("policy hash missing")
	}
	if bundle.Tool.Name != ai.ToolRunView || bundle.Tool.Outcome != "success" {
		t.Fatalf("tool = %+v", bundle.Tool)
	}
	if len(bundle.Tool.Citations) == 0 {
		t.Fatal("expected view/resource citations")
	}
	if bundle.Model.Adapter != "stub-v1" || bundle.Model.Seed != modelSeed {
		t.Fatalf("model = %+v", bundle.Model)
	}
	if bundle.Output.Content == "" || bundle.Output.Context == "" {
		t.Fatal("model output missing")
	}
	if !hasAuditAction(bundle.Audit, audit.ActionExecuteView) {
		t.Fatal("missing execute-view audit")
	}
	if !hasAuditAction(bundle.Audit, audit.ActionExecuteTool) {
		t.Fatal("missing execute-tool audit")
	}
}

func TestAIPipelineDeterministic(t *testing.T) {
	a, err := Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.View.RowHash != b.View.RowHash {
		t.Fatalf("row hash drifted: %s vs %s", a.View.RowHash, b.View.RowHash)
	}
	if a.Output.Content != b.Output.Content {
		t.Fatal("stub model output drifted")
	}
	if a.Policy.Hash != b.Policy.Hash {
		t.Fatal("policy hash drifted")
	}
}

func TestStubModelNoNetwork(t *testing.T) {
	m := &StubModel{Adapter: "stub-v1", Seed: 11}
	resp, err := m.Invoke(context.Background(), ai.ModelRequest{Prompt: "x", Context: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Adapter != "stub-v1" || resp.Content == "" {
		t.Fatalf("%+v", resp)
	}
}
