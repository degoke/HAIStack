package aitest_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/testkit/aitest"
)

func TestHarnessUpdateReindexesSearch(t *testing.T) {
	ctx := context.Background()
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:       true,
		WithSearch:         true,
		WithCore:           true,
		AllowPatientWrite:  true,
		AllowPatientSearch: true,
	})
	_, err := h.Executor.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolUpdateFhirResource,
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
			"patches": map[string]any{
				"name[0].family": "Indexed",
			},
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	old, err := h.Search.Search(ctx, "Patient", aitest.URLValues(map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("search Doe: %v", err)
	}
	if len(old.Resources) != 0 {
		t.Fatalf("expected no Doe matches after update, got %d", len(old.Resources))
	}
	newResult, err := h.Search.Search(ctx, "Patient", aitest.URLValues(map[string]string{"name": "Indexed"}))
	if err != nil {
		t.Fatalf("search Indexed: %v", err)
	}
	if len(newResult.Resources) != 1 || newResult.Resources[0].ID != "pat-jane" {
		t.Fatalf("search Indexed = %#v", newResult.Resources)
	}
}
