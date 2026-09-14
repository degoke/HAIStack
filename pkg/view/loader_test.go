package view

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func TestRegisterViewDefinition(t *testing.T) {
	reg := NewRegistry()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	spec, err := RegisterViewDefinition(reg, PatientSummaryView(), engine)
	if err != nil {
		t.Fatalf("RegisterViewDefinition: %v", err)
	}
	if spec.Name != "patient_summary_view" {
		t.Fatalf("name = %q", spec.Name)
	}
}

func TestRegisterViewDefinitionRejectsNonView(t *testing.T) {
	reg := NewRegistry()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	_, err = RegisterViewDefinition(reg, []byte(`{"resourceType":"Patient","id":"p1"}`), engine)
	if err == nil {
		t.Fatal("expected error")
	}
}
