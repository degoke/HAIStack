package validate_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/sdc"
	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/pkg/validate"
)

func bundledQuestionnaireCatalog(t *testing.T) validate.MemoryProfileCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Questionnaire.json"))
	if err != nil {
		t.Fatalf("read Questionnaire StructureDefinition: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	return catalog
}

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

func TestBaseQuestionnaireProfileAcceptsMultipleRootItems(t *testing.T) {
	catalog := bundledQuestionnaireCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	q := sdc.NewDraft("http://example.org/q", []sdc.Item{
		{LinkID: "a", Type: "string"},
		{LinkID: "b", Type: "string"},
	})
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	env := &types.ResourceEnvelope{ResourceType: "Questionnaire", JSON: raw}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, issue := range result.Issues {
		if issue.Code == "structure" && strings.Contains(issue.Diagnostics, "Questionnaire.item.") {
			t.Fatalf("unexpected structure issue for sibling items: %+v", issue)
		}
	}
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
