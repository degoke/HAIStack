package sdc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func TestMaxLengthRoundTripAndValidation(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"active",
		"item":[{"linkId":"name","type":"string","maxLength":5}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if q.Item[0].MaxLength == nil || *q.Item[0].MaxLength != 5 {
		t.Fatalf("maxLength not decoded: %#v", q.Item[0])
	}
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env.JSON), `"maxLength":5`) {
		t.Fatalf("maxLength not preserved in envelope: %s", env.JSON)
	}

	short := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "Ada"}}}},
	}, ValidationOptions{})
	if len(short.Issue) != 0 {
		t.Fatalf("short answer should pass: %#v", short.Issue)
	}

	long := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "toolong"}}}},
	}, ValidationOptions{})
	if len(long.Issue) != 1 || long.Issue[0].Code != "max" {
		t.Fatalf("expected maxLength issue: %#v", long.Issue)
	}
	if long.Issue[0].FieldPath != "item[name]" {
		t.Fatalf("unexpected field path: %q", long.Issue[0].FieldPath)
	}
}

func TestRegexExtensionRoundTripAndValidation(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"active",
		"item":[{
			"linkId":"code",
			"type":"string",
			"extension":[{"url":"http://hl7.org/fhir/StructureDefinition/regex","valueString":"^[a-z]+$"}]
		}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if q.Item[0].Regex != "^[a-z]+$" {
		t.Fatalf("regex not parsed: %#v", q.Item[0])
	}
	for _, ext := range q.Item[0].Extension {
		if ext.URL == QuestionnaireRegexExtension {
			t.Fatal("regex extension should be absorbed into typed field")
		}
	}

	b, err := json.Marshal(q.Item[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), QuestionnaireRegexExtension) {
		t.Fatalf("regex not emitted: %s", b)
	}

	ok := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "code", Answer: []Answer{{Value: "abc"}}}},
	}, ValidationOptions{})
	if len(ok.Issue) != 0 {
		t.Fatalf("valid regex answer rejected: %#v", ok.Issue)
	}

	bad := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "code", Answer: []Answer{{Value: "A1"}}}},
	}, ValidationOptions{})
	if len(bad.Issue) != 1 || bad.Issue[0].Code != "invariant" {
		t.Fatalf("expected regex invariant issue: %#v", bad.Issue)
	}
}

func TestQuestionnaireConstraintEvaluation(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"active",
		"item":[{
			"linkId":"name",
			"type":"string",
			"required":true,
			"extension":[{
				"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-constraint",
				"extension":[
					{"url":"key","valueId":"name-present"},
					{"url":"requirements","valueString":"Name must be present"},
					{"url":"severity","valueCode":"error"},
					{"url":"expression","valueString":"item.where(linkId='name').answer.value.exists()"}
				]
			}]
		}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Item[0].Constraints) != 1 {
		t.Fatalf("constraint not parsed: %#v", q.Item[0].Constraints)
	}

	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	opts := ValidationOptions{Expressions: FHIRPathExpressions{Engine: engine}}

	missing := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	}, opts)
	if !issueWithDiagnostics(missing, "Name must be present") {
		t.Fatalf("expected constraint failure: %#v", missing.Issue)
	}
	hasRequired := false
	for _, issue := range missing.Issue {
		if issue.Code == "required" {
			hasRequired = true
		}
	}
	if !hasRequired {
		t.Fatalf("expected required failure alongside constraint: %#v", missing.Issue)
	}

	present := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "Ada"}}}},
	}, opts)
	for _, issue := range present.Issue {
		if issue.Diagnostics == "Name must be present" {
			t.Fatalf("constraint should pass when answer present: %#v", present.Issue)
		}
	}
}

func TestConstraintSkippedWhenItemDisabled(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "trigger", Type: "boolean"},
		{
			LinkID:     "dependent",
			Type:       "string",
			EnableWhen: []EnableWhen{{Question: "trigger", Operator: "=", Answer: true}},
			Constraints: []ItemConstraint{{
				Key:          "always-fail",
				Requirements: "should not run",
				Severity:     "error",
				Expression:   "false",
			}},
		},
	})
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	opts := ValidationOptions{Expressions: FHIRPathExpressions{Engine: engine}}
	r := QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "trigger", Answer: []Answer{{Value: false}}}},
	}
	o := ValidateResponse(q, r, opts)
	for _, issue := range o.Issue {
		if issue.Diagnostics == "should not run" {
			t.Fatalf("constraint evaluated on disabled item: %#v", o.Issue)
		}
	}
}

func TestValidateQuestionnaireRejectsInvalidConstraints(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "bad",
		Type:   "group",
		MaxLength: func() *int {
			v := 10
			return &v
		}(),
		Regex: "[",
		Constraints: []ItemConstraint{{
			Key:        "missing-expression",
			Severity:   "fatal",
			Expression: "",
		}},
	}})
	o := ValidateQuestionnaire(q, ValidationOptions{})
	if len(o.Issue) < 3 {
		t.Fatalf("expected multiple definition issues: %#v", o.Issue)
	}
}

func TestRenderExposesConstraints(t *testing.T) {
	max := 12
	q := NewDraft("http://example/q", []Item{{
		LinkID:    "email",
		Type:      "string",
		MaxLength: &max,
		Regex:     "^.+@.+$",
		Constraints: []ItemConstraint{{
			Key:          "has-at",
			Requirements: "must contain @",
			Severity:     "warning",
			Expression:   "true",
		}},
	}})
	model := Render(q, QuestionnaireResponse{Status: "in-progress"})
	if len(model.Fields) != 1 {
		t.Fatalf("fields: %#v", model.Fields)
	}
	f := model.Fields[0]
	if f.MaxLength == nil || *f.MaxLength != 12 || f.Regex != "^.+@.+$" || len(f.Constraints) != 1 {
		t.Fatalf("constraints not exposed: %#v", f)
	}
}

func TestConstraintProviderUnavailable(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "x",
		Type:   "string",
		Constraints: []ItemConstraint{{
			Key:        "c1",
			Expression: "true",
		}},
	}})
	o := ValidateResponse(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []ResponseItem{{LinkID: "x", Answer: []Answer{{Value: "v"}}}},
	}, ValidationOptions{})
	if len(o.Issue) != 1 || o.Issue[0].Code != "exception" {
		t.Fatalf("expected unavailable provider issue: %#v", o.Issue)
	}
}

func issueWithDiagnostics(o Outcome, want string) bool {
	for _, issue := range o.Issue {
		if issue.Diagnostics == want {
			return true
		}
	}
	return false
}

func TestQuestionnaireConstraintAbsorbedFromExtensionOnly(t *testing.T) {
	raw := []byte(`{
		"resourceType":"Questionnaire",
		"url":"http://example/q",
		"status":"active",
		"item":[{
			"linkId":"x",
			"type":"string",
			"maxLength":3,
			"extension":[
				{"url":"http://hl7.org/fhir/StructureDefinition/regex","valueString":"^x+$"},
				{"url":"http://hl7.org/fhir/StructureDefinition/questionnaire-constraint","extension":[
					{"url":"key","valueId":"k"},
					{"url":"expression","valueString":"true"},
					{"url":"severity","valueCode":"warning"}
				]}
			]
		}]
	}`)
	q, err := DecodeQuestionnaire(raw)
	if err != nil {
		t.Fatal(err)
	}
	item := q.Item[0]
	if item.MaxLength == nil || *item.MaxLength != 3 || item.Regex != "^x+$" || len(item.Constraints) != 1 {
		t.Fatalf("item: %#v", item)
	}
	reencoded, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := DecodeQuestionnaire(reencoded)
	if err != nil {
		t.Fatal(err)
	}
	if q2.Item[0].MaxLength == nil || *q2.Item[0].MaxLength != 3 || q2.Item[0].Regex != "^x+$" || len(q2.Item[0].Constraints) != 1 {
		t.Fatalf("round trip failed: %#v", q2.Item[0])
	}
}
