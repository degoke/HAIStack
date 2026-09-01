package validate_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestSliceItemMatchesValueDiscriminator(t *testing.T) {
	sliceEl := validate.ElementDefinition{
		Path:      "Patient.identifier:mrn",
		SliceName: "mrn",
		Pattern:   map[string]interface{}{"system": "urn:mrn"},
	}
	slicing := &validate.ElementSlicing{
		Discriminators: []validate.SliceDiscriminator{{Type: "value", Path: "system"}},
	}
	item := map[string]interface{}{"system": "urn:mrn", "value": "123"}
	if !sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected value discriminator match")
	}
	item["system"] = "urn:other"
	if sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected value discriminator mismatch")
	}
}

func TestSliceItemMatchesTypeDiscriminator(t *testing.T) {
	sliceEl := validate.ElementDefinition{
		Path:      "Patient.extension:str",
		SliceName: "str",
		Types:     []string{"string"},
	}
	slicing := &validate.ElementSlicing{
		Discriminators: []validate.SliceDiscriminator{{Type: "type", Path: "value"}},
	}
	item := map[string]interface{}{"url": "http://example.org", "valueString": "hello"}
	if !sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected type discriminator match for valueString")
	}
	delete(item, "valueString")
	item["valueCode"] = "x"
	if sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected type discriminator mismatch for valueCode")
	}
}

func TestSliceItemMatchesExistsDiscriminator(t *testing.T) {
	sliceEl := validate.ElementDefinition{
		Path:      "Patient.identifier:present",
		SliceName: "present",
		Min:       1,
	}
	slicing := &validate.ElementSlicing{
		Discriminators: []validate.SliceDiscriminator{{Type: "exists", Path: "system"}},
	}
	withSystem := map[string]interface{}{"system": "urn:mrn", "value": "1"}
	if !sliceItemMatchesExported(withSystem, sliceEl, slicing) {
		t.Fatal("expected exists discriminator when system present")
	}
	withoutSystem := map[string]interface{}{"value": "1"}
	if sliceItemMatchesExported(withoutSystem, sliceEl, slicing) {
		t.Fatal("expected exists discriminator mismatch when system absent")
	}
}

func TestSliceItemMatchesPatternDiscriminator(t *testing.T) {
	sliceEl := validate.ElementDefinition{
		Path:      "Patient.identifier:official",
		SliceName: "official",
		Pattern:   map[string]interface{}{"use": "official"},
	}
	slicing := &validate.ElementSlicing{
		Discriminators: []validate.SliceDiscriminator{{Type: "pattern", Path: "use"}},
	}
	item := map[string]interface{}{"use": "official", "value": "abc"}
	if !sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected pattern discriminator match")
	}
	item["use"] = "temp"
	if sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected pattern discriminator mismatch")
	}
}

func TestSliceItemMatchesProfileDiscriminator(t *testing.T) {
	sliceEl := validate.ElementDefinition{
		Path:      "Patient.generalPractitioner:practitioner",
		SliceName: "practitioner",
		Pattern: map[string]interface{}{
			"reference": "Practitioner/123",
		},
	}
	slicing := &validate.ElementSlicing{
		Discriminators: []validate.SliceDiscriminator{{Type: "profile", Path: ""}},
	}
	item := map[string]interface{}{"reference": "Practitioner/123"}
	if !sliceItemMatchesExported(item, sliceEl, slicing) {
		t.Fatal("expected profile/reference discriminator match")
	}
}

func TestUnknownElementWhenParentPathUndefined(t *testing.T) {
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
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","bogusField":"x","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{
		EnforceBaseProfile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "unknown-element" && stringsContains(iss.Diagnostics, "bogusField") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown-element for bogusField, got %+v", result.Issues)
	}
}

func TestInvalidMaxCardinalityReportsStructureIssue(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.name","min":0,"max":"not-a-number","type":[{"code":"HumanName"}]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{EnforceBaseProfile: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, iss := range result.Issues {
		if iss.Code == "structure" && stringsContains(iss.Diagnostics, "invalid max") {
			return
		}
	}
	t.Fatalf("expected invalid max structure issue, got %+v", result.Issues)
}

// sliceItemMatchesExported exposes slice matching for unit tests.
func sliceItemMatchesExported(item map[string]interface{}, sliceEl validate.ElementDefinition, slicing *validate.ElementSlicing) bool {
	return validate.SliceItemMatchesForTest(item, sliceEl, slicing)
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
