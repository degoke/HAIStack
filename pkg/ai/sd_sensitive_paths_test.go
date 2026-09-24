package ai_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
	"github.com/degoke/haistack/pkg/validate"
)

func TestSensitiveFHIRPathsFromPatientStructureDefinition(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	catalog := validate.NewRegistryProfileCatalog(snapshot)
	sd, ok := catalog.GetStructureDefinition(validate.BaseStructureDefinitionURL("Patient"))
	if !ok {
		t.Fatal("missing Patient StructureDefinition")
	}
	fullRules := ai.StructureRulesForMode(ai.PHIModeFull, ai.DefaultPHIStructureRules())
	paths := ai.SensitiveFHIRPathsFromStructureDefinition(sd, fullRules, nil)
	if len(paths) < 10 {
		t.Fatalf("expected sensitive paths from Patient SD in full mode, got %d", len(paths))
	}
	if len(paths) == 0 {
		t.Fatal("expected non-empty path list")
	}
}

func TestFHIRDeidentifier_WithRegistryProfiles(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	profiles := validate.NewRegistryProfileCatalog(snapshot)
	deid, err := ai.NewFHIRDeidentifierWithConfig(ai.FHIRDeidentifierConfig{
		Catalog:  ai.DefaultPHICatalog(),
		Profiles: profiles,
	})
	if err != nil {
		t.Fatalf("NewFHIRDeidentifierWithConfig: %v", err)
	}
	data := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"gender":       "female",
		"name":         []any{map[string]any{"family": "Doe", "given": []any{"Jane"}}},
		"telecom":      []any{map[string]any{"system": "phone", "value": "555"}},
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
	if m["id"] != "pat-1" {
		t.Fatalf("id should remain, got %v", m["id"])
	}
	if m["name"] == nil || m["name"] == "" {
		t.Fatal("expected name subtree present after redaction")
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
}
