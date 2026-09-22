package cql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func demoLibrarySource() string {
	return `
library PatientLogic version '1.0.0'
using FHIR version '4.0.1'
include FHIRHelpers version '4.0.1' called FHIRHelpers

context Patient

define "Patient Given Name":
  First(Patient.name.given)

define "Is Adult":
  AgeInYears() >= 18
`
}

func TestSDCPopulateUsesCQL(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(demoLibrarySource())
	if err != nil {
		t.Fatal(err)
	}
	lib.URL = "http://example.org/Library/PatientLogic"
	provider := NewProvider(eng, StaticLibraries{lib.URL: lib})

	q := sdc.NewDraft("http://example.org/Questionnaire/cql", []sdc.Item{
		{LinkID: "given", Type: "string", InitialExpression: &sdc.Expression{Language: sdc.CQLIdentifierLanguage, Expression: "Patient Given Name"}},
		{LinkID: "adult", Type: "boolean", InitialExpression: &sdc.Expression{Language: sdc.CQLLanguage, Expression: "AgeInYears() >= 18"}},
	})
	q.CQFLibraries = []sdc.CQFLibraryRef{{LibraryCanonical: lib.URL}}

	resp, outcome := sdc.Populate(context.Background(), q, sdc.PopulationContext{
		Subject:  adaPatient(t),
		Provider: sdc.ComposeExpressions(nil, nil, provider),
	})
	if err := sdc.ErrFromOutcome(outcome); err != nil {
		t.Fatalf("populate: %v %#v", err, outcome)
	}
	given := sdcFind(resp.Item, "given")
	if given == nil || len(given.Answer) != 1 || given.Answer[0].Value != "Ada" {
		t.Fatalf("given answer: %#v", given)
	}
	adult := sdcFind(resp.Item, "adult")
	if adult == nil || len(adult.Answer) != 1 || adult.Answer[0].Value != true {
		t.Fatalf("adult answer: %#v", adult)
	}
}

func TestSDCValidateAndRenderUseCQL(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, nil)
	q := sdc.NewDraft("http://example.org/Questionnaire/cql-enable", []sdc.Item{
		{
			LinkID:               "detail",
			Type:                 "string",
			EnableWhenExpression: &sdc.Expression{Language: sdc.CQLLanguage, Expression: "Patient.active"},
			Required:             true,
		},
	})
	r := sdc.QuestionnaireResponse{ResourceType: "QuestionnaireResponse", Status: "in-progress"}
	opts := sdc.ValidationOptions{
		Expressions: sdc.ComposeExpressions(nil, nil, provider),
		Subject:     adaPatient(t),
	}
	outcome := sdc.ValidateResponse(q, r, opts)
	if !sdc.HasErrors(outcome) {
		t.Fatalf("expected required issue when CQL enablement is true: %#v", outcome)
	}

	inactive := adaPatient(t)
	var obj map[string]any
	if err := inactive.DecodeInto(&obj); err != nil {
		t.Fatal(err)
	}
	obj["active"] = false
	b, _ := mustJSON(obj)
	inactive, err := types.NewJSONCodec().ParseJSON("Patient", b)
	if err != nil {
		t.Fatal(err)
	}
	opts.Subject = inactive
	outcome = sdc.ValidateResponse(q, r, opts)
	if sdc.HasErrors(outcome) {
		t.Fatalf("inactive patient should disable required item: %#v", outcome)
	}

	model := sdc.RenderWithOptions(q, r, sdc.ValidationOptions{
		Expressions: sdc.ComposeExpressions(nil, nil, provider),
		Subject:     adaPatient(t),
	})
	if len(model.Fields) != 1 || !model.Fields[0].Enabled {
		t.Fatalf("render enablement: %#v", model.Fields)
	}
}

func TestSDCPopulateMissingLibrary(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, StaticLibraries{})
	q := sdc.NewDraft("http://example.org/Questionnaire/cql", []sdc.Item{
		{LinkID: "given", Type: "string", InitialExpression: &sdc.Expression{Language: sdc.CQLIdentifierLanguage, Expression: "Patient Given Name"}},
	})
	q.CQFLibraries = []sdc.CQFLibraryRef{{LibraryCanonical: "http://example.org/Library/Missing"}}
	_, outcome := sdc.Populate(context.Background(), q, sdc.PopulationContext{
		Subject:  adaPatient(t),
		Provider: sdc.ComposeExpressions(nil, nil, provider),
	})
	if !sdc.HasErrors(outcome) {
		t.Fatal("expected missing library diagnostic")
	}
	if !strings.Contains(outcome.Issue[0].Diagnostics, "not found") {
		t.Fatalf("diagnostic: %#v", outcome.Issue[0])
	}
}

func TestSDCPopulateMissingPatient(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, nil)
	q := sdc.NewDraft("http://example.org/Questionnaire/cql", []sdc.Item{
		{LinkID: "gender", Type: "string", InitialExpression: &sdc.Expression{Language: sdc.CQLLanguage, Expression: "Patient.gender"}},
	})
	_, outcome := sdc.Populate(context.Background(), q, sdc.PopulationContext{
		Provider: sdc.ComposeExpressions(nil, nil, provider),
	})
	if !sdc.HasErrors(outcome) {
		t.Fatal("expected missing patient diagnostic")
	}
	if !errors.Is(errors.New(outcome.Issue[0].Diagnostics), ErrMissingContext) && !strings.Contains(outcome.Issue[0].Diagnostics, "Patient context is missing") {
		t.Fatalf("diagnostic: %#v", outcome.Issue[0])
	}
}

func TestSDCPopulateContainedLibrary(t *testing.T) {
	eng := testEngine(t)
	src := demoLibrarySource()
	q := sdc.NewDraft("http://example.org/Questionnaire/contained-cql", []sdc.Item{
		{LinkID: "given", Type: "string", InitialExpression: &sdc.Expression{Language: sdc.CQLIdentifierLanguage, Expression: "Patient Given Name"}},
	})
	q.Contained = []map[string]any{{
		"resourceType": "Library",
		"id":           "logic",
		"url":          "http://example.org/Library/PatientLogic",
		"name":         "PatientLogic",
		"content": []any{map[string]any{
			"contentType": "text/cql",
			"data":        base64.StdEncoding.EncodeToString([]byte(src)),
		}},
	}}
	q.CQFLibraries = []sdc.CQFLibraryRef{{LibraryCanonical: "http://example.org/Library/PatientLogic"}}
	provider := NewProvider(eng, nil)
	resp, outcome := sdc.Populate(context.Background(), q, sdc.PopulationContext{
		Subject:  adaPatient(t),
		Provider: sdc.ComposeExpressions(nil, nil, provider),
	})
	if err := sdc.ErrFromOutcome(outcome); err != nil {
		t.Fatalf("contained populate: %v %#v", err, outcome)
	}
	given := sdcFind(resp.Item, "given")
	if given == nil || len(given.Answer) != 1 || given.Answer[0].Value != "Ada" {
		t.Fatalf("contained given: %#v", given)
	}
}

func TestProviderApplicationXCQL(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, nil)
	got, err := provider.EvaluateCQL(context.Background(), "Patient.gender", sdc.CQLEvalInput{
		Language: sdc.CQLApplicationXLang,
		Input:    adaPatient(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "female" {
		t.Fatalf("application/x-cql: %#v", got)
	}
}

func TestProviderDoesNotTreatResponseAsPatient(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, nil)
	_, err := provider.EvaluateCQL(context.Background(), "Patient.gender", sdc.QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	})
	if !errors.Is(err, ErrMissingContext) {
		t.Fatalf("expected missing Patient context, got %v", err)
	}
}

func TestExpressionNeedsPatientUsesIdentifiers(t *testing.T) {
	if !expressionNeedsPatient("Patient.gender", sdc.CQLLanguage, nil) {
		t.Fatal("Patient.gender requires Patient context")
	}
	if !expressionNeedsPatient("AgeInYears() >= 18", sdc.CQLLanguage, nil) {
		t.Fatal("AgeInYears requires Patient context")
	}
	if expressionNeedsPatient("true", sdc.CQLLanguage, nil) {
		t.Fatal("literal true does not require Patient context")
	}
	if expressionNeedsPatient("outpatient = true", sdc.CQLLanguage, nil) {
		t.Fatal("outpatient must not force Patient context")
	}
	if expressionNeedsPatient("// patient in a comment\n1 + 1", sdc.CQLLanguage, nil) {
		t.Fatal("comment must not force Patient context")
	}
	lib := &Library{Context: "Patient", Name: "L"}
	if !expressionNeedsPatient("Is Adult", sdc.CQLIdentifierLanguage, []*Library{lib}) {
		t.Fatal("identifier language in Patient-context library requires Patient")
	}
}

func sdcFind(items []sdc.ResponseItem, id string) *sdc.ResponseItem {
	for i := range items {
		if items[i].LinkID == id {
			return &items[i]
		}
	}
	return nil
}

func mustJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
