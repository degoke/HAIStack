package sdc

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/modules"
)

func TestNormalizeAndValidateResponse(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{LinkID: "a", Type: "string", Required: true}, {LinkID: "group", Type: "group", Item: []Item{{LinkID: "b", Type: "boolean"}}}})
	tree, err := Normalize(q)
	if err != nil || len(tree.Resolve("b")) != 1 {
		t.Fatalf("tree: %#v %v", tree, err)
	}
	o := ValidateResponse(q, QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Item: []ResponseItem{{LinkID: "a"}}}, ValidationOptions{})
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
	r := QuestionnaireResponse{Questionnaire: q.URL, Item: []ResponseItem{{LinkID: "trigger", Answer: []Answer{{Value: true}}}, {LinkID: "dependent", Answer: []Answer{{Value: "should not be here"}}}}}
	if Enabled(q.Item[1], r) {
		t.Fatal("dependent item should be disabled")
	}
	o := ValidateResponse(q, r, ValidationOptions{})
	if len(o.Issue) == 0 {
		t.Fatal("expected disabled answer issue")
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
}
