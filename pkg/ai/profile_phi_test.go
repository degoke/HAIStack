package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/validate"
)

func TestFHIRDeidentifier_ProfileAwarePaths(t *testing.T) {
	profileJSON := []byte(`{
		"resourceType": "StructureDefinition",
		"url": "http://example.org/StructureDefinition/patient-extra-phi",
		"type": "Patient",
		"kind": "resource",
		"status": "active",
		"snapshot": {
			"element": [
				{"path": "Patient"},
				{"path": "Patient.extension", "type": [{"code": "Extension"}]},
				{"path": "Patient.extension.valueString", "type": [{"code": "string"}], "mustSupport": true}
			]
		}
	}`)
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{profileJSON})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	deid, deidCfgErr := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		Profiles: catalog,
	})
	if deidCfgErr != nil {
		t.Fatalf("NewFHIRDeidentifierWithConfig: %v", deidCfgErr)
	}
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"meta": map[string]any{
			"profile": []any{"http://example.org/StructureDefinition/patient-extra-phi"},
		},
		"extension": []any{
			map[string]any{"url": "http://example.org/extra", "valueString": "secret"},
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
		t.Fatalf("valueString = %v, want redacted from profile path", ext["valueString"])
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
}

func TestFHIRDeidentifier_NarrativeDivFHIRPath(t *testing.T) {
	deid, err := ai.NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatalf("NewFHIRDeidentifier: %v", err)
	}
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"text": map[string]any{
			"status": "generated",
			"div":    "<div xmlns=\"http://www.w3.org/1999/xhtml\">Jane Doe</div>",
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
	textVal := out.(map[string]any)["text"]
	switch text := textVal.(type) {
	case map[string]any:
		if text["div"] != ai.DefaultRedactedValue {
			t.Fatalf("div = %v, want redacted", text["div"])
		}
	case string:
		if text != ai.DefaultRedactedValue {
			t.Fatalf("text = %v, want fully redacted", text)
		}
	default:
		t.Fatalf("unexpected text type %T", textVal)
	}
}
