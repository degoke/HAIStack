package aipipeline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/audit"
	aipipeline "github.com/degoke/health-ai-stack/research/ai-pipeline"
)

func TestPipelineProvenanceChain(t *testing.T) {
	ctx := context.Background()
	result, err := aipipeline.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ViewRows != 4 {
		t.Fatalf("view rows = %d, want 4 final labs", result.ViewRows)
	}
	if result.DeniedErr == "" {
		t.Fatal("expected denied actor to fail run_view")
	}

	b := result.Bundle
	if b.Pipeline != "research/ai-pipeline" || b.Version != aipipeline.PipelineVersion {
		t.Fatalf("bundle identity = %s %s", b.Pipeline, b.Version)
	}
	if !b.FAIR.Synthetic || b.FAIR.ContainsPHI {
		t.Fatalf("FAIR flags = %+v", b.FAIR)
	}
	if len(b.Inputs) != 9 {
		t.Fatalf("inputs = %d, want 9", len(b.Inputs))
	}
	for _, in := range b.Inputs {
		if in.Hash == "" || in.Ref == "" {
			t.Fatalf("input missing hash/ref: %+v", in)
		}
	}
	if b.View.Name != aipipeline.ViewName || b.View.Version != aipipeline.ViewVersion {
		t.Fatalf("view provenance = %+v", b.View)
	}
	if b.Policy.Hash == "" {
		t.Fatal("policy hash missing")
	}
	if b.Tool.Name != "run_view" {
		t.Fatalf("tool = %q", b.Tool.Name)
	}
	if b.Model.Adapter != "seeded-stub" || b.Model.Seed != aipipeline.StubSeed || b.Model.Content == "" {
		t.Fatalf("model provenance = %+v", b.Model)
	}
	if len(b.Citations) == 0 {
		t.Fatal("expected view/resource citations")
	}

	var sawView, sawTool bool
	for _, ev := range b.Audit {
		if ev.Action == audit.ActionExecuteView {
			sawView = true
		}
		if ev.Action == audit.ActionExecuteTool {
			sawTool = true
		}
	}
	if !sawView || !sawTool {
		t.Fatalf("audit chain missing view or tool events: %+v", b.Audit)
	}

	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"lab-summary:`)) && !strings.Contains(b.Output, "lab-summary:") {
		t.Fatalf("expected stub output, got %q", b.Output)
	}

	again, err := aipipeline.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Bundle.Model.Content != b.Model.Content {
		t.Fatalf("stub output not deterministic: %q vs %q", b.Model.Content, again.Bundle.Model.Content)
	}
	if again.Bundle.Policy.Hash != b.Policy.Hash {
		t.Fatal("policy hash not stable")
	}
	firstJSON, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(again.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("provenance bundle is not byte-stable\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
	for _, ev := range b.Audit {
		if ev.ID == "" {
			t.Fatal("audit event missing stable id")
		}
	}
}
