package validate_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestValidationModeFastSkipsProfileTerminologyBindings(t *testing.T) {
	ctx := context.Background()
	catalog, term := patientGenderCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","maritalStatus":{"coding":[{"system":"urn:marital","code":"bogus"}]},"name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(ctx, env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Terminology:        term,
		Mode:               validate.ValidationModeFast,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("fast mode should not enforce SD bindings, got %+v", result.Issues)
	}
}

func TestValidationModeFullRejectsInvalidGenderBinding(t *testing.T) {
	ctx := context.Background()
	catalog, term := patientGenderCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","maritalStatus":{"coding":[{"system":"urn:marital","code":"bogus"}]},"name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(ctx, env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Terminology:        term,
		Mode:               validate.ValidationModeFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected invalid gender in full mode")
	}
	assertInvalidCode(t, result, err, "terminology-invalid")
}

func TestValidationModeFullRequiresExtensionURL(t *testing.T) {
	catalog := bundledPatientCatalog(t)
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON: []byte(`{
			"resourceType":"Patient",
			"id":"p1",
			"name":[{"family":"Doe"}],
			"extension":[{"valueString":"orphan"}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Mode:               validate.ValidationModeFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected missing extension url to fail in full mode")
	}
	assertInvalidCode(t, result, err, "required")
}

func patientGenderCatalog(t *testing.T) (validate.MemoryProfileCatalog, *terminology.LocalService) {
	t.Helper()
	rawSD := []byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","type":[{"code":"HumanName"}]},
			{"path":"Patient.maritalStatus","min":0,"max":"1","type":[{"code":"CodeableConcept"}],"binding":{"strength":"required","valueSet":"urn:marital"}}
		]}
	}`)
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{rawSD})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st := terminology.NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:marital","version":"1","concept":[{"code":"M"},{"code":"S"}]}`)
	if err := st.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "CodeSystem", CanonicalURL: "urn:marital", Version: "1", Status: "active", ResourceJSON: cs}); err != nil {
		t.Fatal(err)
	}
	if err := terminology.Compile(ctx, st, "s", "", cs); err != nil {
		t.Fatal(err)
	}
	vs := []byte(`{"resourceType":"ValueSet","url":"urn:marital","version":"1","status":"active","compose":{"include":[{"system":"urn:marital"}]}}`)
	if err := st.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "ValueSet", CanonicalURL: "urn:marital", Version: "1", Status: "active", ResourceJSON: vs}); err != nil {
		t.Fatal(err)
	}
	if err := terminology.Compile(ctx, st, "s", "", vs); err != nil {
		t.Fatal(err)
	}
	return catalog, &terminology.LocalService{Store: st, ScopeID: "s"}
}
