package validate_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestNestedExtensionRequiresURL(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"*","type":[{"code":"HumanName"}]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
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
			"extension":[{
				"url":"http://example.org/parent",
				"extension":[{"valueString":"nested"}]
			}]
		}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
		Mode:               validate.ValidationModeFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "required" && stringsContains(iss.Diagnostics, "extension") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected required issue for nested extension url, got %+v", result.Issues)
	}
}

func TestProfileConstraintsEvaluatesDistinctExpressionsForSameKey(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{
		[]byte(`{
			"resourceType":"StructureDefinition",
			"url":"http://hl7.org/fhir/StructureDefinition/Patient",
			"type":"Patient",
			"kind":"resource",
			"snapshot":{"element":[
				{"path":"Patient","min":0,"max":"*"},
				{"path":"Patient.name","min":0,"max":"*","constraint":[
					{"key":"dom-2","severity":"error","expression":"name.exists()","human":"need a name"}
				]}
			]}
		}`),
		[]byte(`{
			"resourceType":"StructureDefinition",
			"url":"http://example.org/Patient-copy",
			"type":"Patient",
			"kind":"resource",
			"snapshot":{"element":[
				{"path":"Patient","min":0,"max":"*"},
				{"path":"Patient.name","min":0,"max":"*","constraint":[
					{"key":"dom-2","severity":"error","expression":"name.family.exists()","human":"need a family name"}
				]}
			]}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var evalCount int
	fp := countingFHIRPath{evals: &evalCount}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","meta":{"profile":["http://example.org/Patient-copy"]},"name":[{"family":"Doe"}]}`),
	}
	_, err = eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile:      true,
		EnforceDeclaredProfiles: true,
		ProfileConstraints:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evalCount != 2 {
		t.Fatalf("eval count = %d, want 2 (same key, different expressions)", evalCount)
	}
}
