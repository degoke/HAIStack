package aitest_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/testkit/aitest"
	"github.com/degoke/haistack/pkg/testkit/fixtures"
)

func TestHarnessSeedsPatientAndBuildsExecutor(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:     true,
		AllowPatientRead: true,
	})
	if h.Executor == nil {
		t.Fatal("executor is nil")
	}
	ctx := context.Background()
	res, err := h.Resources.Read(ctx, "Patient", "pat-jane")
	if err != nil || res == nil {
		t.Fatalf("read patient: %v, %v", res, err)
	}
}

func TestHarnessWithSearch(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:       true,
		WithSearch:         true,
		AllowPatientSearch: true,
	})
	if h.Search == nil {
		t.Fatal("search service is nil")
	}
}

func TestHarnessApprovedUpdateUsesApprovalStoreAndSharedResources(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:          true,
		WithCore:              true,
		AllowPatientWrite:     true,
		WriteRequiresApproval: true,
		ApprovalGranted:       true,
	})
	result, err := h.Executor.ExecuteTool(context.Background(), ai.ToolRequest{
		ToolName: ai.ToolUpdateFhirResource,
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
			"patches": map[string]any{
				"name[0].family": "Updated",
			},
		},
	})
	if err != nil {
		t.Fatalf("approved update: %v", err)
	}
	if result.ApprovalRequired {
		t.Fatal("approved update remained pending")
	}
	updated, err := h.Resources.Read(context.Background(), "Patient", "pat-jane")
	if err != nil {
		t.Fatalf("read updated patient: %v", err)
	}
	if !bytes.Contains(updated.JSON, []byte("Updated")) || bytes.Equal(updated.JSON, fixtures.PatientJane(t).JSON) {
		t.Fatal("shared resource store did not observe core update")
	}
}

func TestHarnessCoreWritesUpdateSharedResources(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{
		SeedPatients:       true,
		WithSearch:         true,
		WithCore:           true,
		AllowPatientWrite:  true,
		AllowPatientSearch: true,
	})
	_, err := h.Executor.ExecuteTool(context.Background(), ai.ToolRequest{
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
	updated, err := h.Resources.Read(context.Background(), "Patient", "pat-jane")
	if err != nil {
		t.Fatalf("read updated patient: %v", err)
	}
	if !bytes.Contains(updated.JSON, []byte("Indexed")) {
		t.Fatal("shared resource store did not observe core update")
	}
}

func TestHarnessModelAdapterIsWired(t *testing.T) {
	h := aitest.NewHarness(t, aitest.Options{})
	response, err := h.Executor.InvokeModel(context.Background(), ai.ToolRequest{ModelHint: "local"}, "prompt", "context")
	if err != nil {
		t.Fatalf("InvokeModel: %v", err)
	}
	if response == nil || response.Adapter != "test-model" || response.Content != "ok-local" {
		t.Fatalf("response = %+v", response)
	}
}
