package sdc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func TestTier1ExtensionsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire","url":"http://example/q","status":"active",
		"item":[{
			"linkId":"age","type":"integer",
			"extension":[
				{"url":"http://hl7.org/fhir/StructureDefinition/minLength","valueInteger":2},
				{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-minValue","valueInteger":0},
				{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-maxValue","valueInteger":120},
				{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-minOccurs","valueInteger":1},
				{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-maxOccurs","valueInteger":3}
			]
		}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	item := q.Item[0]
	if item.MinLength == nil || *item.MinLength != 2 {
		t.Fatalf("minLength: %#v", item.MinLength)
	}
	if item.MinValue == nil || item.MaxValue == nil || item.MinOccurs == nil || item.MaxOccurs == nil {
		t.Fatalf("bounds/occurs not parsed: %#v", item)
	}
	b, _ := json.Marshal(q)
	if !strings.Contains(string(b), "questionnaire-minValue") {
		t.Fatalf("minValue not emitted: %s", b)
	}
}

func TestTier2ReferenceAndQuantityValidation(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{
			LinkID:             "patient",
			Type:               "reference",
			ReferenceResources: []string{"Patient"},
		},
		{
			LinkID:      "weight",
			Type:        "quantity",
			Unit:        &Coding{System: "http://unitsofmeasure.org", Code: "kg"},
			UnitOptions: []Coding{{System: "http://unitsofmeasure.org", Code: "kg"}},
		},
	})
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{
			{LinkID: "patient", Answer: []Answer{{Value: map[string]any{"reference": "Observation/1"}}}},
			{LinkID: "weight", Answer: []Answer{{Value: map[string]any{"value": 70, "code": "lb", "system": "http://unitsofmeasure.org"}}}},
		},
	}, ValidationOptions{})
	if len(o.Issue) < 2 {
		t.Fatalf("expected reference and unit issues: %#v", o.Issue)
	}
}

func TestTier3RenderMetadata(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:            "color",
		Type:              "choice",
		ChoiceOrientation: "horizontal",
		OptionExclusive:   true,
		UsageMode:         "capture",
		DisplayCategory:   "instructions",
		SupportLinks:      []SupportLink{{URL: "http://help", Label: "Help"}},
		AnswerOption:      []AnswerOption{{Value: Coding{Code: "red"}, ValueType: "Coding", OptionPrefix: "A"}},
	}})
	model := Render(q, QuestionnaireResponse{Status: "in-progress"})
	f := model.Fields[0]
	if f.ChoiceOrientation != "horizontal" || !f.OptionExclusive || f.UsageMode != "capture" || f.DisplayCategory != "instructions" || len(f.SupportLinks) != 1 {
		t.Fatalf("metadata not exposed: %#v", f)
	}
	if len(f.Options) != 1 || f.Options[0].OptionPrefix != "A" {
		t.Fatalf("option prefix not preserved: %#v", f.Options)
	}
}

func TestTier4QuestionnaireExtensions(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire","url":"http://example/q","status":"active",
		"extension":[
			{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-launchContext","extension":[
				{"url":"name","valueCode":"patient"},{"url":"type","valueCode":"Patient"}
			]},
			{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-variable","extension":[
				{"url":"name","valueCode":"score"},{"url":"expression","valueExpression":{"language":"text/fhirpath","expression":"1"}}
			]},
			{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-signatureRequired","valueBoolean":true}
		],
		"item":[{"linkId":"x","type":"string"}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.LaunchContexts) != 1 || len(q.Variables) != 1 || !q.SignatureRequired {
		t.Fatalf("questionnaire extensions: %#v", q)
	}
}

func TestTier5ExtractionMetadata(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		SourceStructureMap:    "http://example/map",
		AdditionalDefinitions: []string{"http://example/def"},
		ObservationExtract:    true,
	}
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env.JSON), "sourceStructureMap") {
		t.Fatalf("missing extraction metadata: %s", env.JSON)
	}
}

func TestTier6ResponseExtensions(t *testing.T) {
	raw := []byte(`{
		"resourceType":"QuestionnaireResponse","status":"in-progress",
		"extension":[
			{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-responseAuthor","valueReference":{"reference":"Practitioner/1"}},
			{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-completionMode","valueCode":"entered-in-error"}
		]
	}`)
	r, err := DecodeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Author) != 1 || r.CompletionMode != "entered-in-error" {
		t.Fatalf("response extensions: %#v", r)
	}
}

func TestOptionExclusiveValidation(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "tags", Type: "choice", Repeats: true, OptionExclusive: true,
		AnswerOption: []AnswerOption{{Value: Coding{Code: "a"}, ValueType: "Coding"}, {Value: Coding{Code: "b"}, ValueType: "Coding"}},
	}})
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{{LinkID: "tags", Answer: []Answer{
			{Value: Coding{Code: "a"}},
			{Value: Coding{Code: "b"}},
		}}},
	}, ValidationOptions{})
	if len(o.Issue) == 0 || o.Issue[0].Code != "max" {
		t.Fatalf("expected optionExclusive issue: %#v", o.Issue)
	}
}

func TestSignatureRequiredOnQuestionnaire(t *testing.T) {
	q := Questionnaire{ResourceType: "Questionnaire", URL: "http://example/q", Status: "active", SignatureRequired: true}
	o := ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "completed"}, ValidationOptions{})
	if len(o.Issue) == 0 {
		t.Fatal("expected signature required issue")
	}
}

func TestQuestionItemTypeSupported(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "q1", Type: "question", Item: []Item{{LinkID: "nested", Type: "string"}}}})
	o := ValidateQuestionnaire(q, ValidationOptions{})
	if len(o.Issue) != 0 {
		t.Fatalf("question item type should be valid: %#v", o.Issue)
	}
}

func TestAnswerValueSetExpansionValidation(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "color", Type: "open-choice", AnswerValueSet: "http://example/vs"}})
	term := StaticTerminology{Codes: map[string]map[string]string{"urn:sys": {"red": "Red"}}}
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "color", Answer: []Answer{{Value: "blue"}}}},
	}, ValidationOptions{Terminology: term})
	if len(o.Issue) == 0 {
		t.Fatal("expected value set validation issue")
	}
}

func TestRequiredExpressionValidation(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := NewDraft("http://example/q", []Item{{
		LinkID: "detail",
		Type:   "string",
		RequiredExpression: &Expression{Language: "text/fhirpath", Expression: "true"},
	}})
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "detail"}},
	}, ValidationOptions{
		Expressions: FHIRPathExpressions{Engine: engine},
	})
	if len(o.Issue) == 0 {
		t.Fatal("expected required expression issue")
	}
}
