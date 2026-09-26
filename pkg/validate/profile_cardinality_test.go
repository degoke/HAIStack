package validate_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/sdc"
	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/pkg/validate"
)

func TestCardinalityManyQuestionnaireRootItems(t *testing.T) {
	catalog := bundledQuestionnaireCatalog(t)
	eng := newValidateEngine(t, catalog)
	items := make([]sdc.Item, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, sdc.Item{LinkID: string(rune('a' + i)), Type: "string"})
	}
	q := sdc.NewDraft("http://example.org/q", items)
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	result := validateEnvelope(t, eng, "Questionnaire", raw)
	assertNoStructureIssuePrefix(t, result, "Questionnaire.item.")
}

func TestCardinalityNestedRepeatUnderRepeat(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Questionnaire",
		"type":"Questionnaire",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Questionnaire","min":0,"max":"*"},
			{"path":"Questionnaire.item","min":0,"max":"*","type":[{"code":"BackboneElement"}]},
			{"path":"Questionnaire.item.linkId","min":1,"max":"1","type":[{"code":"string"}]},
			{"path":"Questionnaire.item.item","min":0,"max":"*","type":[{"code":"BackboneElement"}]},
			{"path":"Questionnaire.item.item.linkId","min":1,"max":"1","type":[{"code":"string"}]},
			{"path":"Questionnaire.item.item.type","min":1,"max":"1","type":[{"code":"code"}]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng := newValidateEngine(t, catalog)
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"status":"draft",
		"item":[
			{"linkId":"g1","type":"group","item":[
				{"linkId":"q1","type":"string"},
				{"linkId":"q2","type":"boolean"}
			]},
			{"linkId":"g2","type":"group","item":[{"linkId":"q3","type":"string"}]}
		]
	}`)
	result := validateEnvelope(t, eng, "Questionnaire", raw)
	assertNoStructureIssuePrefix(t, result, "Questionnaire.item.")
}

func TestCardinalityFlagsMissingChildOnOneSiblingOnly(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Questionnaire",
		"type":"Questionnaire",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Questionnaire","min":0,"max":"*"},
			{"path":"Questionnaire.item","min":0,"max":"*","type":[{"code":"BackboneElement"}]},
			{"path":"Questionnaire.item.linkId","min":1,"max":"1","type":[{"code":"string"}]},
			{"path":"Questionnaire.item.type","min":1,"max":"1","type":[{"code":"code"}]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng := newValidateEngine(t, catalog)
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"status":"draft",
		"item":[
			{"linkId":"ok","type":"string"},
			{"type":"string"}
		]
	}`)
	result := validateEnvelope(t, eng, "Questionnaire", raw)
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "required" && strings.Contains(iss.Diagnostics, "Questionnaire.item.linkId") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected required linkId on single bad item, got %+v", result.Issues)
	}
}

func TestContainedResourceUsesOwnStructureDefinition(t *testing.T) {
	patientSD := `{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Patient",
		"type":"Patient",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Patient","min":0,"max":"*"},
			{"path":"Patient.maritalStatus","min":1,"max":"1","type":[{"code":"CodeableConcept"}]}
		]}
	}`
	observationSD, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(patientSD), observationSD})
	if err != nil {
		t.Fatal(err)
	}
	eng := newValidateEngine(t, catalog)
	raw := []byte(`{
		"resourceType":"Patient",
		"id":"p1",
		"maritalStatus":{"coding":[{"system":"urn:marital","code":"M"}]},
		"contained":[
			{"resourceType":"Observation","id":"o1","status":"final","code":{"text":"weight"},"valueQuantity":{"value":70,"unit":"kg"}}
		]
	}`)
	result := validateEnvelope(t, eng, "Patient", raw)
	for _, iss := range result.Issues {
		if iss.Code == "required" && strings.Contains(iss.Diagnostics, "maritalStatus") && strings.Contains(iss.Diagnostics, "Observation") {
			t.Fatalf("parent Patient cardinality applied to contained Observation: %+v", iss)
		}
		if iss.Code == "required" && strings.Contains(iss.Diagnostics, "Patient.maritalStatus") {
			t.Fatalf("unexpected maritalStatus issue with valid patient: %+v", iss)
		}
	}
}

func TestChoiceCardinalityMaxOnePerInstance(t *testing.T) {
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://hl7.org/fhir/StructureDefinition/Observation",
		"type":"Observation",
		"kind":"resource",
		"snapshot":{"element":[
			{"path":"Observation","min":0,"max":"*"},
			{"path":"Observation.value[x]","min":0,"max":"1","type":[{"code":"Quantity"},{"code":"string"}]}
		]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	eng := newValidateEngine(t, catalog)
	raw := []byte(`{
		"resourceType":"Observation",
		"status":"final",
		"code":{"text":"x"},
		"valueQuantity":{"value":1,"unit":"mg"},
		"valueString":"also-set"
	}`)
	result := validateEnvelope(t, eng, "Observation", raw)
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "structure" && strings.Contains(iss.Diagnostics, "Observation.value[x]") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected max 1 choice violation, got %+v", result.Issues)
	}
}

func newValidateEngine(t *testing.T, catalog validate.MemoryProfileCatalog) validate.Engine {
	t.Helper()
	eng, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func validateEnvelope(t *testing.T, eng validate.Engine, resourceType string, raw []byte) *validate.ValidationResult {
	t.Helper()
	env := &types.ResourceEnvelope{ResourceType: resourceType, JSON: raw}
	result, err := eng.Validate(context.Background(), env, validate.ValidateOptions{EnforceBaseProfile: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertNoStructureIssuePrefix(t *testing.T, result *validate.ValidationResult, prefix string) {
	t.Helper()
	for _, iss := range result.Issues {
		if iss.Code == "structure" && strings.Contains(iss.Diagnostics, prefix) {
			t.Fatalf("unexpected structure issue: %+v", iss)
		}
	}
}
