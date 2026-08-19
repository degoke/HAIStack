package sdc

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type testQuestionnaireResolverFunc func(context.Context, string) (Questionnaire, error)

func (f testQuestionnaireResolverFunc) Resolve(ctx context.Context, canonical string) (Questionnaire, error) {
	return f(ctx, canonical)
}

func TestNormalizeAndValidateResponse(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "a", Type: "string", Required: true}, {LinkID: "group", Type: "group", Item: []Item{{LinkID: "b", Type: "boolean"}}}})
	tree, err := Normalize(q)
	if err != nil || len(tree.Resolve("b")) != 1 {
		t.Fatalf("tree: %#v %v", tree, err)
	}
	o := ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress", Item: []ResponseItem{{LinkID: "a"}}}, ValidationOptions{})
	if len(o.Issue) != 1 || o.Issue[0].Code != "required" {
		t.Fatalf("unexpected outcome: %#v", o)
	}
}

type testExpressionProvider struct{}

func (testExpressionProvider) Evaluate(_ context.Context, e Expression, _ any) ([]any, error) {
	if e.Expression == "1" {
		return []any{"calculated"}, nil
	}
	return nil, nil
}
func TestCalculatedExpressionConverges(t *testing.T) {
	q := NewDraft("", []Item{{LinkID: "x", Type: "string", CalculatedExpression: &Expression{Language: "text/fhirpath", Expression: "1"}}})
	r := EvaluateCalculated(context.Background(), q, QuestionnaireResponse{}, testExpressionProvider{}, CalculatedOptions{})
	if !r.Converged || len(findResponse(r.Response.Item, "x").Answer) != 1 {
		t.Fatalf("result: %#v", r)
	}
}

func TestCalculatedDependencyCycleIsDiagnosed(t *testing.T) {
	q := NewDraft("", []Item{{LinkID: "a", Type: "integer", CalculatedExpression: &Expression{Language: "text/fhirpath", Expression: "b"}}, {LinkID: "b", Type: "integer", CalculatedExpression: &Expression{Language: "text/fhirpath", Expression: "a"}}})
	r := EvaluateCalculated(context.Background(), q, QuestionnaireResponse{}, testExpressionProvider{}, CalculatedOptions{})
	if r.Converged || len(r.Diagnostics) == 0 {
		t.Fatalf("expected cycle diagnostic: %#v", r)
	}
}

func TestDefinitionExtractionIsTransactionAndDeterministic(t *testing.T) {
	q := NewDraft("", nil)
	r := QuestionnaireResponse{Item: []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "Ada"}}}}}
	x, e := DefinitionExtractor{Mappings: []DefinitionMap{{LinkID: "name", ResourceType: "Patient", Path: "name"}}}.Extract(context.Background(), q, r)
	if e != nil || x.Bundle == nil || string(x.Bundle.JSON) == "" || !bytes.Contains(x.Bundle.JSON, []byte(`"type":"transaction"`)) {
		t.Fatalf("extract: %#v %v", x, e)
	}
}

func TestEnableWhenAndDisabledValidation(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "trigger", Type: "boolean"}, {LinkID: "dependent", Type: "string", Required: true, EnableWhen: []EnableWhen{{Question: "trigger", Operator: "=", Answer: false}}}})
	r := QuestionnaireResponse{Questionnaire: q.URL, Status: "in-progress", Item: []ResponseItem{{LinkID: "trigger", Answer: []Answer{{Value: true}}}, {LinkID: "dependent", Answer: []Answer{{Value: "should not be here"}}}}}
	if Enabled(q.Item[1], r) {
		t.Fatal("dependent item should be disabled")
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) == 0 {
		t.Fatal("expected disabled answer issue")
	}
	if !strings.Contains(o.Issue[0].Diagnostics, "trigger =") {
		t.Fatalf("disabled diagnostic lacks rule context: %#v", o.Issue[0])
	}
}

func TestPopulationExpressionFailureIsReported(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "x", Type: "string", InitialExpression: &Expression{Language: "text/cql", Expression: "x"}}})
	_, o := Populate(context.Background(), q, PopulationContext{Provider: CQLExpressions{}})
	if len(o.Issue) == 0 {
		t.Fatal("expected unsupported expression diagnostic")
	}
}

func TestSDCModuleLoadsThroughInstallerLoader(t *testing.T) {
	path := filepath.Join("..", "..", "modules", "sdc")
	m, e := modules.NewLoader().Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Definitions) != 7 {
		t.Fatalf("loaded %d definitions", len(m.Definitions))
	}
}

func TestEnvelopeFirstResourceBoundary(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "x", Type: "string"}})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	env, err = QuestionnaireResource(env)
	if err != nil {
		t.Fatal(err)
	}
	if env.ResourceType != "Questionnaire" {
		t.Fatalf("resource type=%q", env.ResourceType)
	}
	if _, e := DecodeQuestionnaireResource(env); e != nil {
		t.Fatal(e)
	}
	if _, e := ParseR4(env); e != nil {
		t.Fatalf("R4 proto parse: %v", e)
	}
	if _, e := QuestionnaireResource(&types.ResourceEnvelope{ResourceType: "Questionnaire", JSON: []byte(`{"resourceType":"Patient"}`)}); e == nil {
		t.Fatal("expected resource boundary to reject mismatched JSON")
	}
}

func TestValidateResponseReportsAbsentRequiredItems(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "required", Type: "string", Required: true}})
	o := ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}, ValidationOptions{})
	if len(o.Issue) != 1 || o.Issue[0].Code != "required" {
		t.Fatalf("outcome: %#v", o)
	}
}

func TestValidateResponseReportsNestedRequiredItems(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "group", Type: "group", Item: []Item{{LinkID: "required", Type: "string", Required: true}}}})
	r := QuestionnaireResponse{Status: "in-progress", Item: []ResponseItem{{LinkID: "group"}}}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 1 || o.Issue[0].Code != "required" {
		t.Fatalf("outcome: %#v", o)
	}
}

func TestAnswerTypeValidationRejectsInvalidNumbersAndUnknownTypes(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "integer", Type: "integer"},
		{LinkID: "decimal", Type: "decimal"},
		{LinkID: "quantity", Type: "quantity"},
	})
	r := QuestionnaireResponse{Status: "in-progress", Item: []ResponseItem{
		{LinkID: "integer", Answer: []Answer{{Value: "not-an-integer"}}},
		{LinkID: "decimal", Answer: []Answer{{Value: "not-a-number"}}},
		{LinkID: "quantity", Answer: []Answer{{Value: "not-a-quantity"}}},
	}}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 3 {
		t.Fatalf("expected three type issues, got %#v", o)
	}

	r.Item[0].Answer[0].Value = 1.5
	o = ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) != 3 {
		t.Fatalf("fractional integer accepted: %#v", o)
	}
}

func TestEnableWhenUsesFHIRAnswerKeysAndRoundTrips(t *testing.T) {
	var rule EnableWhen
	if err := json.Unmarshal([]byte(`{"question":"trigger","operator":"=","answerBoolean":false}`), &rule); err != nil {
		t.Fatal(err)
	}
	if value, ok := rule.Answer.(bool); !ok || value {
		t.Fatalf("answer: %#v", rule.Answer)
	}
	b, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"answerBoolean":false`) || strings.Contains(string(b), "valueBoolean") {
		t.Fatalf("invalid enableWhen JSON: %s", b)
	}
}

func TestQuestionnaireProjectionRoundTripsExtensionsAndHash(t *testing.T) {
	q := NewDraft("http://example/q", []Item{
		{LinkID: "trigger", Type: "boolean"},
		{
			LinkID:            "dependent",
			Type:              "decimal",
			EnableWhen:        []EnableWhen{{Question: "trigger", Operator: "=", Answer: false}},
			Initial:           []Answer{{Value: 70.5}},
			InitialExpression: &Expression{Language: "text/fhirpath", Expression: "Patient.active"},
			TextRef:           "#question-help",
			Media:             []Attachment{{ContentType: "image/png", URL: "http://example/image.png"}},
		},
	})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env.JSON), `"initialExpression"`) || strings.Contains(string(env.JSON), `"valueBoolean"`) {
		t.Fatalf("projection emitted non-FHIR fields: %s", env.JSON)
	}
	if !strings.Contains(string(env.JSON), `"valueExpression"`) || !strings.Contains(string(env.JSON), `"answerBoolean":false`) {
		t.Fatalf("projection omitted canonical fields: %s", env.JSON)
	}
	q2, err := DecodeQuestionnaireResource(env)
	if err != nil {
		t.Fatal(err)
	}
	if q2.Item[1].InitialExpression == nil || q2.Item[1].TextRef != "#question-help" || len(q2.Item[1].Media) != 1 {
		t.Fatalf("projection fields were not restored: %#v", q2.Item[1])
	}
	env2, err := ProjectionEnvelope(q2)
	if err != nil {
		t.Fatal(err)
	}
	if env.Hash == "" || env.Hash != env2.Hash {
		t.Fatalf("hash changed across projection round trip: %q != %q\nfirst=%s\nsecond=%s", env.Hash, env2.Hash, env.JSON, env2.JSON)
	}
	if _, err := ParseR4(env2); err != nil {
		t.Fatalf("R4 parse: %v\n%s", err, env2.JSON)
	}
}

func TestNestedEnableWhenExistsAndRender(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "group",
		Type:   "group",
		Item: []Item{
			{LinkID: "trigger", Type: "boolean"},
			{LinkID: "dependent", Type: "string", EnableWhen: []EnableWhen{{Question: "trigger", Operator: "=", Answer: true}}},
		},
	}})
	r := QuestionnaireResponse{Item: []ResponseItem{{LinkID: "group", Item: []ResponseItem{{LinkID: "trigger", Answer: []Answer{{Value: true}}}, {LinkID: "dependent", Answer: []Answer{{Value: "ok"}}}}}}}
	if !Enabled(q.Item[0].Item[1], r) {
		t.Fatal("nested dependent item should be enabled")
	}
	model := Render(q, r)
	for _, field := range model.Fields {
		if field.LinkID == "dependent" && len(field.Answers) != 1 {
			t.Fatalf("nested answer missing from render model: %#v", field)
		}
	}

	existsFalse := Item{LinkID: "missing-ok", Type: "string", EnableWhen: []EnableWhen{{Question: "not-answered", Operator: "exists", Answer: false}}}
	if !Enabled(existsFalse, QuestionnaireResponse{}) {
		t.Fatal("exists=false should enable when the item is absent")
	}
}

func TestFHIRPathProviderAdaptsSDCInputs(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	q := NewDraft("http://example/q", []Item{{LinkID: "active", Type: "boolean", InitialExpression: &Expression{Language: "text/fhirpath", Expression: "Patient.active"}}})
	subject, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{"resourceType":"Patient","active":true}`))
	if err != nil {
		t.Fatal(err)
	}
	r, o := Populate(context.Background(), q, PopulationContext{Subject: subject, Provider: FHIRPathExpressions{Engine: engine}})
	if len(o.Issue) != 0 || r == nil || len(r.Item) != 1 || len(r.Item[0].Answer) != 1 || r.Item[0].Answer[0].Value != true {
		t.Fatalf("FHIRPath population failed: response=%#v outcome=%#v", r, o)
	}
}

func TestAssembleQuestionnaireResourceIsEnvelopeFirstAndRecursive(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "module", Type: "group", Definition: "http://example/module"}})
	env, err := ProjectionEnvelope(q)
	if err != nil {
		t.Fatal(err)
	}
	resolver := testQuestionnaireResolverFunc(func(ctx context.Context, canonical string) (Questionnaire, error) {
		if canonical == "http://example/module" {
			return NewDraft(canonical, []Item{{LinkID: "child", Type: "group", Definition: "http://example/nested"}}), nil
		}
		return NewDraft(canonical, []Item{{LinkID: "leaf", Type: "string"}}), nil
	})
	assembled, outcome := AssembleQuestionnaireResource(context.Background(), env, resolver)
	if len(outcome.Issue) != 0 || assembled == nil {
		t.Fatalf("assembly: env=%#v outcome=%#v", assembled, outcome)
	}
	result, err := DecodeQuestionnaireResource(assembled)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Item) != 1 || result.Item[0].LinkID != "leaf" {
		t.Fatalf("recursive assembly did not expand nested module: %#v", result.Item)
	}
}

func TestDefinitionExtractionUsesUniqueFullURLsForNestedRepeatedMappings(t *testing.T) {
	r := QuestionnaireResponse{Item: []ResponseItem{{LinkID: "group", Item: []ResponseItem{{LinkID: "name", Answer: []Answer{{Value: "Ada"}}}, {LinkID: "name", Answer: []Answer{{Value: "Grace"}}}}}}}
	result, err := (DefinitionExtractor{Mappings: []DefinitionMap{{LinkID: "name", ResourceType: "Patient", Path: "name"}, {LinkID: "name", ResourceType: "Observation", Path: "valueString"}}}).Extract(context.Background(), Questionnaire{}, r)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Entry []struct {
			FullURL string `json:"fullUrl"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(result.Bundle.JSON, &bundle); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range bundle.Entry {
		if entry.FullURL == "" || seen[entry.FullURL] {
			t.Fatalf("duplicate or empty fullUrl: %#v", bundle.Entry)
		}
		seen[entry.FullURL] = true
	}
	if len(bundle.Entry) != 4 {
		t.Fatalf("entries=%d, want 4", len(bundle.Entry))
	}
}

func TestSequentialAdaptiveEngine(t *testing.T) {
	q := NewDraft("http://example/adaptive", []Item{
		{LinkID: "first", Text: "First question", Type: "boolean"},
		{LinkID: "second", Text: "Second question", Type: "string", EnableWhen: []EnableWhen{{Question: "first", Operator: "=", Answer: true}}},
	})
	engine := SequentialAdaptiveEngine{Questionnaires: []Questionnaire{q}}
	session := &AdaptiveSession{ID: "s1", Questionnaire: q.URL, Response: QuestionnaireResponse{Status: "in-progress"}}
	next, err := engine.NextQuestion(context.Background(), session)
	if err != nil || next == nil || next.LinkID != "first" {
		t.Fatalf("first next question: %#v err=%v", next, err)
	}
	next, err = engine.SubmitAnswer(context.Background(), session, ResponseItem{LinkID: "first", Answer: []Answer{{Value: true}}})
	if err != nil || next == nil || next.LinkID != "second" {
		t.Fatalf("second next question: %#v err=%v", next, err)
	}
	if matches, err := engine.Search(context.Background(), AdaptiveSearchRequest{Query: "first question"}); err != nil || len(matches) != 1 {
		t.Fatalf("adaptive search: %#v err=%v", matches, err)
	}
}
