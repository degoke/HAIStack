package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMergeDeidentifyMetaFields(t *testing.T) {
	full := []byte(`{"resourceType":"Patient","id":"p1","meta":{"security":[{"system":"http://terminology.hl7.org/CodeSystem/v3-ConfidentialityCode","code":"R"}],"profile":["http://example.org/StructureDefinition/patient"]},"name":[{"family":"Doe"}],"gender":"male"}`)
	filtered := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"name":         []any{map[string]any{"family": "Doe"}},
	}
	if err := mergeDeidentifyMetaFields(filtered, full); err != nil {
		t.Fatal(err)
	}
	meta := filtered["meta"].(map[string]any)
	if meta["security"] == nil || meta["profile"] == nil {
		t.Fatalf("meta = %v", meta)
	}
	if _, ok := filtered["gender"]; ok {
		t.Fatal("gender should not be merged from full resource")
	}
}

func TestReadDeidentifyStrictWithNarrowAllowedFields(t *testing.T) {
	deid, err := NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	full := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"meta": map[string]any{
			"security": []any{map[string]any{
				"system": V3ConfidentialityCodeSystem,
				"code":   "R",
			}},
		},
		"name":   []any{map[string]any{"family": "Doe"}},
		"gender": "male",
	}
	filtered := projectResourceMap(full, []string{"name", "gender"})
	if err := mergeDeidentifyMetaFields(filtered, mustJSON(full)); err != nil {
		t.Fatal(err)
	}
	out, redactions, err := deid.Deidentify(context.Background(), DeidentifyRequest{
		ToolName:     ToolReadFhirResource,
		ResourceType: "Patient",
		Data:         filtered,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
	m := projectResourceMap(out.(map[string]any), []string{"name", "gender"})
	if m["gender"] != DefaultRedactedValue {
		t.Fatalf("gender = %v (strict mode should redact)", m["gender"])
	}
	if _, ok := m["meta"]; ok {
		t.Fatal("meta should be stripped after projection")
	}
}

func TestProjectIncludedResourceMapFullRead(t *testing.T) {
	full := map[string]any{
		"resourceType": "Organization",
		"id":           "org-1",
		"name":         "Acme",
		"gender":       "n/a",
	}
	out := projectIncludedResourceMap(full, nil)
	if out["name"] != "Acme" {
		t.Fatalf("full read allowlist should keep included fields, got %v", out)
	}
	narrow := projectIncludedResourceMap(full, []string{"name"})
	if narrow["name"] != "Acme" {
		t.Fatal("expected name")
	}
	if _, ok := narrow["gender"]; ok {
		t.Fatal("gender should be stripped with narrow allowlist")
	}
}

func TestFHIRDeidentifierUnsupportedTool(t *testing.T) {
	deid, err := NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = deid.Deidentify(context.Background(), DeidentifyRequest{
		ToolName: "custom_tool",
		Data:     map[string]any{"x": 1},
	})
	if !errors.Is(err, ErrUnsupportedDeidentifyTool) {
		t.Fatalf("err = %v", err)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
