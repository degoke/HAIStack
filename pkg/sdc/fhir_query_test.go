package sdc

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type stubFHIRQuerySearch struct {
	lastType   string
	lastParams url.Values
	resources  []*types.ResourceEnvelope
	err        error
}

func (s *stubFHIRQuerySearch) Search(_ context.Context, resourceType string, params url.Values) ([]*types.ResourceEnvelope, error) {
	s.lastType = resourceType
	s.lastParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.resources, nil
}

func TestParseFHIRQuery(t *testing.T) {
	rt, params, err := parseFHIRQuery("Patient?active=true&name=Ada")
	if err != nil {
		t.Fatal(err)
	}
	if rt != "Patient" || params.Get("active") != "true" || params.Get("name") != "Ada" {
		t.Fatalf("unexpected parse: %s %#v", rt, params)
	}

	rt, params, err = parseFHIRQuery("http://example/fhir/Patient?active=true")
	if err != nil || rt != "Patient" || params.Get("active") != "true" {
		t.Fatalf("url parse: %s %#v %v", rt, params, err)
	}

	_, _, err = parseFHIRQuery("")
	if err == nil {
		t.Fatal("expected empty query error")
	}
}

func TestSubstituteFHIRQueryConstants(t *testing.T) {
	query, err := substituteFHIRQueryConstants(
		"Observation?subject=%subject&code=8867-4",
		map[string]any{"subject": map[string]any{"reference": "Patient/pat-1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if query != "Observation?subject=Patient%2Fpat-1&code=8867-4" {
		t.Fatalf("unexpected substitution: %s", query)
	}

	query, err = substituteFHIRQueryConstants(
		"Observation?subject=%patient",
		map[string]any{"subject": map[string]any{"reference": "Patient/pat-1"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if query != "Observation?subject=Patient%2Fpat-1" {
		t.Fatalf("unexpected patient alias substitution: %s", query)
	}

	_, err = substituteFHIRQueryConstants("Patient?subject=%missing", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "%missing") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}

func TestSubstituteURLEncodesSpecialCharacters(t *testing.T) {
	query, err := substituteFHIRQueryConstants(
		"Observation?subject=%subject",
		map[string]any{"subject": "Patient/abc|def"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if query != "Observation?subject=Patient%2Fabc%7Cdef" {
		t.Fatalf("unexpected encoding: %s", query)
	}
}

func TestSearchFHIRQueryProviderExecutesQuery(t *testing.T) {
	search := &stubFHIRQuerySearch{
		resources: []*types.ResourceEnvelope{{
			ResourceType: "Patient",
			ID:           "pat-1",
			JSON:         []byte(`{"resourceType":"Patient","id":"pat-1","active":true}`),
		}},
	}
	provider := SearchFHIRQueryProvider{Search: search}
	values, err := provider.ExecuteFHIRQuery(context.Background(), "Patient?active=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	if search.lastType != "Patient" || search.lastParams.Get("active") != "true" {
		t.Fatalf("search not called as expected: %s %#v", search.lastType, search.lastParams)
	}
	if len(values) != 1 {
		t.Fatalf("expected one resource, got %#v", values)
	}
}

func TestComposeExpressionsRoutesFHIRQuery(t *testing.T) {
	search := &stubFHIRQuerySearch{
		resources: []*types.ResourceEnvelope{{ResourceType: "Patient", ID: "pat-1"}},
	}
	provider := ComposeExpressions(nil, SearchFHIRQueryProvider{Search: search}, nil)
	values, err := provider.Evaluate(context.Background(), Expression{
		Language:   FHIRQueryLanguage,
		Expression: "Patient?active=true",
	}, QuestionnaireResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("expected routed FHIR Query result, got %#v", values)
	}
}

func TestContextExpressionOnRender(t *testing.T) {
	search := &stubFHIRQuerySearch{
		resources: []*types.ResourceEnvelope{{
			ResourceType: "Observation",
			ID:           "obs-1",
			JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","status":"final"}`),
		}},
	}
	provider := ComposeExpressions(
		FHIRPathExpressions{Engine: mustFHIRPathEngine(t)},
		SearchFHIRQueryProvider{Search: search},
		nil,
	)
	subject := map[string]any{"resourceType": "Patient", "id": "pat-1"}
	q := NewDraft("http://example/q", []Item{{
		LinkID: "recent-vitals",
		Type:   "string",
		ContextExpressions: []ContextExpression{{
			Label: "Recent vitals",
			Expression: Expression{
				Language:   FHIRQueryLanguage,
				Expression: "Observation?subject=%subject&category=vital-signs",
			},
		}},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	}, ValidationOptions{
		Expressions: provider,
		Subject:     subject,
	})
	if len(model.Fields[0].ContextResources) != 1 {
		t.Fatalf("expected context resources: %#v", model.Fields[0])
	}
	if len(model.Fields[0].ContextResources[0].Resources) != 1 {
		t.Fatalf("expected evaluated resources: %#v", model.Fields[0].ContextResources)
	}
	if search.lastParams.Get("subject") != "Patient/pat-1" {
		t.Fatalf("subject not substituted: %#v", search.lastParams)
	}
	if model.Fields[0].ContextResources[0].Label != "Recent vitals" {
		t.Fatalf("unexpected label: %#v", model.Fields[0].ContextResources[0])
	}
}

func TestContextExpressionFHIRQueryOnlyCompose(t *testing.T) {
	search := &stubFHIRQuerySearch{
		resources: []*types.ResourceEnvelope{{ResourceType: "Patient", ID: "pat-1"}},
	}
	provider := ComposeExpressions(nil, SearchFHIRQueryProvider{Search: search}, nil)
	q := NewDraft("http://example/q", []Item{{
		LinkID: "x",
		Type:   "string",
		ContextExpressions: []ContextExpression{{
			Expression: Expression{Language: FHIRQueryLanguage, Expression: "Patient?active=true"},
		}},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	}, ValidationOptions{Expressions: provider})
	if len(model.Fields[0].ContextResources) != 1 || len(model.Fields[0].ContextResources[0].Resources) != 1 {
		t.Fatalf("expected context resources: %#v", model.Fields[0].ContextResources)
	}
}

func TestContextExpressionSearchErrorAddsDiagnostic(t *testing.T) {
	search := &stubFHIRQuerySearch{err: context.Canceled}
	provider := ComposeExpressions(nil, SearchFHIRQueryProvider{Search: search}, nil)
	q := NewDraft("http://example/q", []Item{{
		LinkID: "x",
		Type:   "string",
		ContextExpressions: []ContextExpression{{
			Expression: Expression{Language: FHIRQueryLanguage, Expression: "Patient?active=true"},
		}},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	}, ValidationOptions{Expressions: provider})
	if len(model.Fields[0].Issues) == 0 {
		t.Fatal("expected diagnostic when search fails")
	}
}

func TestContextExpressionWithoutProviderAddsDiagnostic(t *testing.T) {
	q := NewDraft("http://example/q", []Item{{
		LinkID: "recent-vitals",
		Type:   "string",
		ContextExpressions: []ContextExpression{{
			Expression: Expression{Language: FHIRQueryLanguage, Expression: "Observation?subject=Patient/1"},
		}},
	}})
	model := RenderWithOptions(q, QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	}, ValidationOptions{})
	if len(model.Fields[0].ContextResources) != 0 {
		t.Fatalf("expected no resources without provider: %#v", model.Fields[0].ContextResources)
	}
	if len(model.Fields[0].Issues) == 0 {
		t.Fatal("expected diagnostic when provider is unavailable")
	}
}

func mustFHIRPathEngine(t *testing.T) fhirpath.Engine {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
