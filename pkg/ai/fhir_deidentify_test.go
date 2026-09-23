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

func TestFHIRDeidentifier_DeepExtensionValueString(t *testing.T) {
	deid := ai.NewFHIRDeidentifier(nil)
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"extension": []any{
			map[string]any{"url": "http://example.org/note", "valueString": "secret note"},
		},
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
	ext := m["extension"].([]any)[0].(map[string]any)
	if ext["valueString"] != ai.DefaultRedactedValue {
		t.Fatalf("valueString = %v, want redacted", ext["valueString"])
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
}

func TestFHIRDeidentifier_SecurityLabelStrict(t *testing.T) {
	deid := ai.NewFHIRDeidentifier(nil)
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"gender":       "female",
		"meta": map[string]any{
			"security": []any{
				map[string]any{
					"system": ai.V3ConfidentialityCodeSystem,
					"code":   "R",
				},
			},
		},
	}
	out, _, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName:     ai.ToolReadFhirResource,
		ResourceType: "Patient",
		Data:         data,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
	m := out.(map[string]any)
	if m["gender"] != ai.DefaultRedactedValue {
		t.Fatalf("gender = %v, want redacted under strict confidentiality", m["gender"])
	}
	if m["id"] != "pat-1" {
		t.Fatalf("id should remain for grounding, got %v", m["id"])
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
