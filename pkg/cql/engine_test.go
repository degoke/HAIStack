package cql

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngine(Config{
		Now: func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func adaPatient(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
		"resourceType": "Patient",
		"id": "ada",
		"active": true,
		"name": [{"use": "official", "family": "Lovelace", "given": ["Ada", "Augusta"]}],
		"gender": "female",
		"birthDate": "2000-01-15"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestEvalSimpleExpressionAgainstPatient(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Patient.gender", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "female" {
		t.Fatalf("gender: %#v", got)
	}

	got, err = eng.Eval(context.Background(), "First(Patient.name.given)", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("given: %#v", got)
	}

	got, err = eng.Eval(context.Background(), "Patient.active", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("active: %#v", got)
	}
}

func TestEvalAgeInYears(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "AgeInYears() >= 18", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("adult: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "AgeInYears()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(26) {
		t.Fatalf("age: %#v", got)
	}
}

func TestEvalMissingPatientContext(t *testing.T) {
	eng := testEngine(t)
	_, err := eng.Eval(context.Background(), "Patient.gender", EvalContext{})
	if !errors.Is(err, ErrMissingContext) {
		t.Fatalf("expected missing context, got %v", err)
	}
	_, err = eng.Eval(context.Background(), "AgeInYears()", EvalContext{})
	if !errors.Is(err, ErrMissingContext) {
		t.Fatalf("expected missing context for AgeInYears, got %v", err)
	}
}

func TestEvalLibraryDefine(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library PatientName version '1.0.0'
using FHIR version '4.0.1'
include FHIRHelpers version '4.0.1' called FHIRHelpers

context Patient

define "Patient Given Name":
  First(Patient.name.given)

define "Is Adult":
  AgeInYears() >= 18
`)
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.Eval(context.Background(), `"Patient Given Name"`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("define: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Is Adult", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("is adult: %#v", got)
	}
}

func TestEvalIdentifierLanguageMissingDefine(t *testing.T) {
	eng := testEngine(t)
	_, err := eng.Eval(context.Background(), "Missing", EvalContext{
		Language: "text/cql.identifier",
		Patient:  adaPatient(t),
	})
	if !errors.Is(err, ErrExpressionNotFound) {
		t.Fatalf("expected missing expression, got %v", err)
	}
}

func TestEvalIfAndBoolean(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "if Patient.gender = 'female' then 'F' else 'M'", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "F" {
		t.Fatalf("if: %#v", got)
	}
}

func TestEvalRetrieve(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"text": "Heart rate"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{obs},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "[Observation].where(status = 'final').count()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("retrieve count: %#v", got)
	}
}

func TestParseLibraryResource(t *testing.T) {
	src := "library Demo version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": true\n"
	env, err := types.NewJSONCodec().ParseJSON("Library", []byte(`{
		"resourceType": "Library",
		"id": "demo",
		"url": "http://example.org/Library/Demo",
		"name": "Demo",
		"version": "1.0.0",
		"status": "active",
		"type": {"coding": [{"code": "logic-library"}]},
		"content": [{"contentType": "text/cql", "data": "`+base64.StdEncoding.EncodeToString([]byte(src))+`"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cqlSrc, url, name, version, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://example.org/Library/Demo" || name != "Demo" || version != "1.0.0" {
		t.Fatalf("meta: %s %s %s", url, name, version)
	}
	if !strings.Contains(cqlSrc, `define "X"`) {
		t.Fatalf("source: %s", cqlSrc)
	}
}

func TestELMOnlyLibraryIsUnsupported(t *testing.T) {
	env, err := types.NewJSONCodec().ParseJSON("Library", []byte(`{
		"resourceType": "Library",
		"url": "http://example.org/Library/ELM",
		"content": [{"contentType": "application/elm+json", "data": "e30="}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = EnvelopeLibrary(env)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported ELM-only library, got %v", err)
	}
}

func TestUnsupportedFunction(t *testing.T) {
	eng := testEngine(t)
	_, err := eng.ParseLibrary(`library X version '1'
using FHIR version '4.0.1'
context Patient
define function "Foo"(x Integer): x
`)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported function, got %v", err)
	}
}

func TestCommentsAndArithmetic(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "1 + 2 * 3", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("arith: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "/* comment */ 2 + 2 // trailing", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(4) {
		t.Fatalf("comments: %#v", got)
	}
}
