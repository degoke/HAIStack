package validate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func bundledPatientCatalog(t *testing.T) validate.MemoryProfileCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Patient.json"))
	if err != nil {
		t.Fatalf("read Patient StructureDefinition: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	return catalog
}

func TestBundledPatientProfileUsesSnapshot(t *testing.T) {
	catalog := bundledPatientCatalog(t)
	sd, ok := catalog.GetStructureDefinition(validate.BaseStructureDefinitionURL("Patient"))
	if !ok {
		t.Fatal("missing Patient profile")
	}
	if !sd.UseSnapshot {
		t.Fatal("expected snapshot-based validation")
	}
	if len(sd.Elements) < 40 {
		t.Fatalf("elements = %d, want full snapshot", len(sd.Elements))
	}
}

func TestBasePatientProfileRejectsUnknownElement(t *testing.T) {
	catalog := bundledPatientCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"bogusField":"nope",
			"name":[{"family":"Doe"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		ProfileConstraints: true,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid, got %+v", result.Issues)
	}
	assertInvalidCode(t, result, err, "unknown-element")
}

func TestBasePatientProfileAcceptsMinimalPatient(t *testing.T) {
	catalog := bundledPatientCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"pat-1",
			"name":[{"family":"Doe"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		ProfileConstraints: true,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid minimal Patient, got %+v", result.Issues)
	}
}
