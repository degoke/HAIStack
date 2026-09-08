package sdc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type stubReferenceResolver struct {
	resources map[string]map[string]any
}

func (s stubReferenceResolver) ResolveReference(_ context.Context, ref Reference) (map[string]any, error) {
	key := ref.Reference
	if key == "" && ref.Type != "" {
		key = ref.Type
	}
	if res, ok := s.resources[key]; ok {
		return res, nil
	}
	return nil, context.Canceled
}

func TestReferenceProfileAndFilterValidation(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID:            "patient",
		Type:              "reference",
		ReferenceProfiles: []string{"http://example/StructureDefinition/MyPatient"},
		ReferenceFilter:   "name.exists()",
	}})
	resolver := stubReferenceResolver{resources: map[string]map[string]any{
		"Patient/1": {
			"resourceType": "Patient",
			"meta":         map[string]any{"profile": []string{"http://example/StructureDefinition/MyPatient"}},
			"name":         []map[string]any{{"family": "Doe"}},
		},
		"Patient/2": {
			"resourceType": "Patient",
			"meta":         map[string]any{"profile": []string{"http://example/StructureDefinition/Other"}},
		},
	}}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	opts := ValidationOptions{
		References:  resolver,
		Expressions: FHIRPathExpressions{Engine: engine},
	}
	ok := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "patient", Answer: []Answer{{Value: map[string]any{"reference": "Patient/1"}}}}},
	}, opts)
	if len(ok.Issue) != 0 {
		t.Fatalf("expected valid reference: %#v", ok.Issue)
	}
	badProfile := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "patient", Answer: []Answer{{Value: map[string]any{"reference": "Patient/2"}}}}},
	}, opts)
	if len(badProfile.Issue) == 0 {
		t.Fatal("expected profile validation issue")
	}
}

func TestLaunchContextValidation(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		LaunchContexts: []LaunchContextDef{{Name: "patient", Type: []string{"Patient"}}},
	}
	o := ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{})
	if len(o.Issue) == 0 {
		t.Fatal("expected missing launch context issue")
	}
	o = ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{
		LaunchContext: map[string]any{"patient": map[string]any{"resourceType": "Observation"}},
	})
	if len(o.Issue) == 0 || !strings.Contains(o.Issue[0].Diagnostics, "launch context patient") {
		t.Fatalf("expected launch context type issue: %#v", o.Issue)
	}
	o = ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{
		LaunchContext: map[string]any{"patient": map[string]any{"resourceType": "Patient", "id": "1"}},
	})
	if len(o.Issue) != 0 {
		t.Fatalf("expected valid launch context: %#v", o.Issue)
	}
}

func TestVariableContextInValidation(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		Variables: []QuestionnaireVariable{{
			Name: "threshold",
			Expression: Expression{Language: "text/fhirpath", Expression: "5"},
		}},
		Item: []Item{{
			LinkID: "detail",
			Type:   "string",
			Constraints: []ItemConstraint{{
				Key:        "min-threshold",
				Expression: "%threshold = 5",
				Severity:   "error",
			}},
		}},
	}
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "detail", Answer: []Answer{{Value: "ok"}}}},
	}, ValidationOptions{Expressions: FHIRPathExpressions{Engine: engine}})
	for _, issue := range o.Issue {
		if issue.Code == "invariant" && strings.Contains(issue.Diagnostics, "constraint") {
			t.Fatalf("variable substitution should satisfy constraint: %#v", o.Issue)
		}
	}
}

func TestUsageModeSemantics(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "banner", Type: "string", UsageMode: "display", Required: true},
		{LinkID: "name", Type: "string", UsageMode: "capture", Required: true},
		{LinkID: "summary", Type: "string", UsageMode: "display-non-empty"},
	})
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{
			{LinkID: "banner", Answer: []Answer{{Value: "hello"}}},
			{LinkID: "summary", Answer: []Answer{{Value: "shown"}}},
		},
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	hasRequired := false
	hasForbidden := false
	for _, issue := range o.Issue {
		if issue.Code == "required" && strings.Contains(issue.FieldPath, "banner") {
			t.Fatalf("display item should not be required: %#v", o.Issue)
		}
		if issue.Code == "required" && strings.Contains(issue.FieldPath, "name") {
			hasRequired = true
		}
		if issue.Code == "forbidden" && strings.Contains(issue.FieldPath, "banner") {
			hasForbidden = true
		}
	}
	if !hasRequired {
		t.Fatalf("expected required issue for capture item: %#v", o.Issue)
	}
	if !hasForbidden {
		t.Fatalf("expected forbidden issue for display item answer: %#v", o.Issue)
	}
	model := Render(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "completed", Item: r.Item})
	byID := map[string]FieldState{}
	for _, f := range model.Fields {
		byID[f.LinkID] = f
	}
	if byID["banner"].Visible != true || byID["banner"].Enabled != false {
		t.Fatalf("display field visibility: %#v", byID["banner"])
	}
	if byID["summary"].Visible != true || byID["summary"].Enabled != false {
		t.Fatalf("display-non-empty field visibility: %#v", byID["summary"])
	}
}

func TestResponseReasonAndIsSubjectExtensions(t *testing.T) {
	raw := []byte(`{
		"resourceType":"QuestionnaireResponse","status":"in-progress",
		"extension":[{"url":"http://hl7.org/fhir/StructureDefinition/questionnaireresponse-reason","valueCodeableConcept":{"text":"referral"}}],
		"item":[{"linkId":"group","extension":[{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaireresponse-isSubject","valueBoolean":true}]}]
	}`)
	r, err := DecodeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ResponseReasons) != 1 || r.ResponseReasons[0].Text != "referral" {
		t.Fatalf("response reason: %#v", r.ResponseReasons)
	}
	if !r.Item[0].IsSubject {
		t.Fatal("expected response item isSubject")
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), "questionnaireresponse-reason") {
		t.Fatalf("response reason not emitted: %s", b)
	}
}

func TestIsSubjectAndKeyboardItemExtensions(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire","url":"http://example/q","status":"active",
		"item":[{"linkId":"subject","type":"group","extension":[
			{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-isSubject","valueBoolean":true},
			{"url":"http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-keyboard","valueCoding":{"code":"numeric"}}
		]}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Item[0].IsSubject || q.Item[0].InputKeyboard != "numeric" {
		t.Fatalf("item extensions: %#v", q.Item[0])
	}
}

func TestQuestionnaireExtractorUsesMetadata(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		SourceStructureMap: "http://example/map",
		Item:               []Item{{LinkID: "name", Type: "string", Definition: "http://hl7.org/fhir/StructureDefinition/Patient#Patient.name.given"}},
	}
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "Ada"}}}},
	}
	result, err := QuestionnaireExtractor{
		StructureMap: StructureMapExtractor{Run: func(_ context.Context, q Questionnaire, _ QuestionnaireResponse) ([]json.RawMessage, error) {
			if q.SourceStructureMap != "http://example/map" {
				t.Fatalf("expected sourceStructureMap in extractor: %s", q.SourceStructureMap)
			}
			raw, _ := json.Marshal(map[string]any{"resourceType": "Patient", "name": []map[string]any{{"given": []string{"Ada"}}}})
			return []json.RawMessage{raw}, nil
		}},
	}.Extract(context.Background(), q, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle == nil || len(result.Diagnostics) == 0 {
		t.Fatalf("expected bundle and diagnostics: %#v", result)
	}
}

func TestParametersParsing(t *testing.T) {
	raw := []byte(`{"resourceType":"Parameters","parameter":[{"name":"subject","valueReference":{"reference":"Patient/1"}},{"name":"patient","resource":{"resourceType":"Patient","id":"1"}}]}`)
	env, err := types.NewJSONCodec().ParseJSON("Parameters", raw)
	if err != nil {
		t.Fatal(err)
	}
	params := ParseOperationParameters(env)
	if params.Subject == nil {
		t.Fatal("expected subject")
	}
	if params.LaunchContext["patient"] == nil {
		t.Fatalf("expected patient launch context: %#v", params.LaunchContext)
	}
}

func TestDefinitionExtractionFromItemDefinition(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		ObservationExtract: true,
		Item: []Item{{
			LinkID:     "value",
			Type:       "decimal",
			Definition: "http://hl7.org/fhir/StructureDefinition/Observation#Observation.valueQuantity.value",
		}},
	}
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "value", Answer: []Answer{{Value: 42.0}}}},
	}
	result, err := QuestionnaireExtractor{}.Extract(context.Background(), q, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle == nil || !strings.Contains(string(result.Bundle.JSON), "Observation") {
		t.Fatalf("expected observation extraction bundle: %s", result.Bundle.JSON)
	}
}

func TestUsageModeRespectsResponseStatus(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "score", Type: "string", UsageMode: "display"}})
	capture := Render(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"})
	if capture.Fields[0].Visible {
		t.Fatal("display item should be hidden during capture")
	}
	display := Render(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "completed"})
	if !display.Fields[0].Visible {
		t.Fatal("display item should be visible when completed")
	}
}

func TestAnswerOptionToggleExpression(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := NewDraft("http://example/q", []Item{{
		LinkID: "color",
		Type:   "choice",
		AnswerOption: []AnswerOption{
			{Value: Coding{Code: "red"}, ValueType: "Coding"},
			{Value: Coding{Code: "blue"}, ValueType: "Coding", ToggleExpression: &Expression{Language: "text/fhirpath", Expression: "false"}},
		},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{
		Expressions: FHIRPathExpressions{Engine: engine},
	})
	if len(model.Fields[0].Options) != 2 || !model.Fields[0].Options[1].Disabled {
		t.Fatalf("expected disabled option: %#v", model.Fields[0].Options)
	}
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "color", Answer: []Answer{{Value: Coding{Code: "blue"}, ValueType: "Coding"}}}},
	}, ValidationOptions{Expressions: FHIRPathExpressions{Engine: engine}})
	for _, issue := range o.Issue {
		if issue.Code == "code-invalid" && strings.Contains(issue.Diagnostics, "disabled") {
			return
		}
	}
	t.Fatalf("expected disabled option validation issue: %#v", o.Issue)
}

func TestItemPopulationContextScopesDescendants(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		Item: []Item{{
			LinkID: "group",
			Type:   "group",
			ItemPopulationContext: &Expression{
				Language: "text/fhirpath", Expression: "'ctx'", Name: "ctx",
			},
			Item: []Item{{
				LinkID: "detail",
				Type:   "string",
				Constraints: []ItemConstraint{{
					Key: "ctx-check", Expression: "%ctx = 'ctx'", Severity: "error",
				}},
			}},
		}},
	}
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{{LinkID: "group", Item: []ResponseItem{{
			LinkID: "detail", Answer: []Answer{{Value: "ok"}},
		}}}},
	}, ValidationOptions{Expressions: FHIRPathExpressions{Engine: engine}})
	for _, issue := range o.Issue {
		if issue.Code == "invariant" {
			t.Fatalf("item population context should satisfy descendant constraint: %#v", o.Issue)
		}
	}
}

func TestDefinitionExtractGroup(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active",
		Item: []Item{{
			LinkID: "patient",
			Type:   "group",
			DefinitionExtract: &DefinitionExtractContext{
				Definition: "http://hl7.org/fhir/StructureDefinition/Patient",
			},
			Item: []Item{{
				LinkID:     "given",
				Type:       "string",
				Definition: "http://hl7.org/fhir/StructureDefinition/Patient#Patient.name.given",
			}},
		}},
	}
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{{LinkID: "patient", Item: []ResponseItem{{
			LinkID: "given", Answer: []Answer{{Value: "Ada"}},
		}}}},
	}
	result, err := QuestionnaireExtractor{}.Extract(context.Background(), q, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle == nil || !strings.Contains(string(result.Bundle.JSON), "Ada") {
		t.Fatalf("expected definitionExtract bundle: %s", result.Bundle.JSON)
	}
}

func TestSequentialEntryModeValidation(t *testing.T) {
	q := Questionnaire{
		ResourceType: "Questionnaire", URL: "http://example/q", Status: "active", EntryMode: "sequential",
		Item: []Item{
			{LinkID: "first", Type: "string", Required: true},
			{LinkID: "second", Type: "string", Required: true},
		},
	}
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "second", Answer: []Answer{{Value: "late"}}}},
	}, ValidationOptions{})
	for _, issue := range o.Issue {
		if issue.Code == "invariant" && strings.Contains(issue.Diagnostics, "sequential") {
			return
		}
	}
	t.Fatalf("expected sequential entry mode issue: %#v", o.Issue)
}

func TestUnitOpenAllowsCustomUnit(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "weight",
		Type:   "quantity",
		UnitOpen: "options-or-string",
		UnitOptions: []Coding{{Code: "kg", System: "http://unitsofmeasure.org"}},
	}})
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item: []ResponseItem{{LinkID: "weight", Answer: []Answer{{Value: map[string]any{"value": 70, "code": "lb", "system": "http://unitsofmeasure.org"}}}}},
	}, ValidationOptions{})
	for _, issue := range o.Issue {
		if issue.Code == "code-invalid" && strings.Contains(issue.Diagnostics, "unit") {
			t.Fatalf("unitOpen should allow custom unit: %#v", o.Issue)
		}
	}
}

func TestCandidateExpressionOnRender(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := NewDraft("http://example/q", []Item{{
		LinkID: "ref",
		Type:   "reference",
		CandidateExpression: &Expression{Language: "text/fhirpath", Expression: "'Patient/1'"},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{
		Expressions: FHIRPathExpressions{Engine: engine},
	})
	if len(model.Fields[0].Candidates) == 0 {
		t.Fatalf("expected candidates: %#v", model.Fields[0])
	}
}
