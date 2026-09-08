package validate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

const haiPatientProfileJSON = `{
  "resourceType": "StructureDefinition",
  "url": "http://haistack.example.org/fhir/StructureDefinition/hai-patient",
  "type": "Patient",
  "kind": "resource",
  "differential": {
    "element": [
      {"path": "Patient", "min": 0, "max": "*"},
      {"path": "Patient.identifier", "min": 1, "max": "*"},
      {"path": "Patient.identifier.system", "min": 1, "max": "1"},
      {"path": "Patient.identifier.value", "min": 1, "max": "1"}
    ]
  }
}`

func haiPatientCatalog(t *testing.T) validate.MemoryProfileCatalog {
	t.Helper()
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(haiPatientProfileJSON)})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	return catalog
}

func TestProfileRejectsPatientMissingIdentifier(t *testing.T) {
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: haiPatientCatalog(t)})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"meta":{"profile":["http://haistack.example.org/fhir/StructureDefinition/hai-patient"]},
			"name":[{"family":"Doe"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{EnforceDeclaredProfiles: true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result for missing identifier")
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "required" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected required issue, got %+v", result.Issues)
	}
	outcome := validate.ToOperationOutcome(result)
	if outcome == nil || len(outcome.Issue) == 0 || outcome.Issue[0].Code != "required" {
		t.Fatalf("OperationOutcome = %+v", outcome)
	}
}

func TestProfileAcceptsPatientWithIdentifier(t *testing.T) {
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: haiPatientCatalog(t)})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"meta":{"profile":["http://haistack.example.org/fhir/StructureDefinition/hai-patient"]},
			"identifier":[{"system":"http://example.org/mrn","value":"1"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{EnforceDeclaredProfiles: true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %+v", result.Issues)
	}
}

func TestProfileNotEnforcedByDefault(t *testing.T) {
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: haiPatientCatalog(t)})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"meta":{"profile":["http://haistack.example.org/fhir/StructureDefinition/hai-patient"]}
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("structural-only validation should pass, got %+v", result.Issues)
	}
}

func TestUnknownProfileIsRejectedWhenEnforced(t *testing.T) {
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: haiPatientCatalog(t)})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"meta":{"profile":["http://example.org/unknown"]}
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{EnforceDeclaredProfiles: true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	assertInvalidCode(t, result, err, "unknown-profile")
}

func TestLoadProfileCatalogFromDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hai-patient.json")
	if err := os.WriteFile(path, []byte(haiPatientProfileJSON), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDir: %v", err)
	}
	if _, ok := catalog.GetStructureDefinition("http://haistack.example.org/fhir/StructureDefinition/hai-patient"); !ok {
		t.Fatal("expected hai-patient profile in catalog")
	}
}

func TestProfileCatalogFromCompiledIG(t *testing.T) {
	dir := filepath.Join("..", "..", "modules", "core", "ig")
	catalog, err := validate.LoadProfileCatalogFromDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromDir: %v", err)
	}
	if _, ok := catalog.GetStructureDefinition("http://haistack.example.org/fhir/StructureDefinition/hai-patient"); !ok {
		t.Fatal("compiled IG is missing hai-patient")
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	invalid := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1","meta":{"profile":["http://haistack.example.org/fhir/StructureDefinition/hai-patient"]}}`),
	}
	result, err := eng.Validate(context.Background(), invalid, validate.ValidateOptions{EnforceDeclaredProfiles: true})
	assertInvalidCode(t, result, err, "required")
}
