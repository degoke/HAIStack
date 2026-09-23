package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestFHIRDeidentifier_ReadPatient(t *testing.T) {
	deid := ai.NewFHIRDeidentifier(nil)
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"gender":       "female",
		"name":         []any{map[string]any{"family": "Doe"}},
		"telecom":      []any{map[string]any{"value": "555-0100"}},
	}
	out, redactions, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName:     ai.ToolReadFhirResource,
		ResourceType: "Patient",
		Data:         data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
	m := out.(map[string]any)
	if m["gender"] != "female" {
		t.Fatalf("gender should remain, got %v", m["gender"])
	}
	if m["name"] != ai.DefaultRedactedValue {
		t.Fatalf("name = %v, want redacted", m["name"])
	}
	if m["telecom"] != ai.DefaultRedactedValue {
		t.Fatalf("telecom = %v, want redacted", m["telecom"])
	}
	if len(redactions) < 2 {
		t.Fatalf("redactions = %v, want name and telecom", redactions)
	}
}

func TestFHIRDeidentifier_SearchBundle(t *testing.T) {
	deid := ai.NewFHIRDeidentifier(nil)
	data := map[string]any{
		"resourceType": "Patient",
		"resources": []any{
			map[string]any{
				"resourceType": "Patient",
				"id":           "p1",
				"name":         []any{map[string]any{"family": "Doe"}},
			},
		},
	}
	_, redactions, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName:     ai.ToolSearchFhirResources,
		ResourceType: "Patient",
		Data:         data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions on search resources")
	}
}

func TestFHIRDeidentifier_ViewRows(t *testing.T) {
	deid := ai.NewFHIRDeidentifier(nil)
	data := map[string]any{
		"viewName": "patient_summary_view",
		"rows": []any{
			map[string]any{
				"id":     "p1",
				"name":   "Jane Doe",
				"gender": "female",
			},
		},
	}
	_, redactions, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName: ai.ToolRunView,
		Data:     data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected view column redactions")
	}
}

func TestExecutor_ReadPatient_WithFHIRDeidentifier(t *testing.T) {
	h := newTestHarness(t, harnessOptions{
		seedPatients:     true,
		allowPatientRead: true,
	})
	h.policy.Read["Patient"] = ai.ReadTypePolicy{Deidentify: true}
	exec, err := ai.NewExecutor(ai.Config{
		Resources:  h.resources,
		Policy:     h.policy,
		Audit:      h.audit,
		Deidentify: ai.NewFHIRDeidentifier(nil),
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	ctx := context.Background()
	res, err := exec.ExecuteTool(ctx, ai.ToolRequest{
		ToolName: ai.ToolReadFhirResource,
		Actor:    "agent-1",
		Input: map[string]any{
			"resourceType": "Patient",
			"id":           "pat-jane",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	data := dataMap(t, res.Data)
	if data["name"] != ai.DefaultRedactedValue {
		t.Fatalf("name = %v, want redacted", data["name"])
	}
	if len(res.Redactions) == 0 {
		t.Fatal("expected redactions on tool result")
	}
}
