package cql

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
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

func TestResourceMatchesPatientRequiresSubject(t *testing.T) {
	unscoped, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"x","status":"final"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resourceMatchesPatient(unscoped, "Patient/ada") {
		t.Fatal("observation without subject must not match a patient retrieve")
	}
	other, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"y","subject":{"reference":"Patient/other"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resourceMatchesPatient(other, "Patient/ada") {
		t.Fatal("observation for another patient must not match")
	}
	mine, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"z","subject":{"reference":"Patient/ada"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !resourceMatchesPatient(mine, "Patient/ada") {
		t.Fatal("expected matching observation")
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

func TestNewEngineLeavesFHIRPathOptional(t *testing.T) {
	eng := testEngine(t)
	if eng.fhirpath != nil {
		t.Fatal("NewEngine must not construct a FHIRPath engine when Config.FHIRPath is nil")
	}
}

type tracingFHIRPath struct {
	inner fhirpath.Engine
	calls []string
}

func (t *tracingFHIRPath) Compile(expr string) (fhirpath.CompiledExpression, error) {
	return t.inner.Compile(expr)
}
func (t *tracingFHIRPath) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	t.calls = append(t.calls, expr)
	return t.inner.Eval(ctx, expr, resource)
}
func (t *tracingFHIRPath) EvalWithEnv(ctx context.Context, expr string, resource any, env map[string]any) ([]fhirpath.Value, error) {
	t.calls = append(t.calls, expr)
	return t.inner.EvalWithEnv(ctx, expr, resource, env)
}
func (t *tracingFHIRPath) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	t.calls = append(t.calls, expr)
	return t.inner.EvalBool(ctx, expr, resource)
}
func (t *tracingFHIRPath) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	t.calls = append(t.calls, expr)
	return t.inner.EvalString(ctx, expr, resource)
}

func TestEvalUsesConfiguredFHIRPath(t *testing.T) {
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	trace := &tracingFHIRPath{inner: fp}
	eng, err := NewEngine(Config{
		FHIRPath: trace,
		Now:      func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "Patient.gender", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "female" {
		t.Fatalf("gender: %#v", got)
	}
	if len(trace.calls) == 0 {
		t.Fatal("expected Config.FHIRPath to evaluate resource member access")
	}
	found := false
	for _, c := range trace.calls {
		if c == "gender" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("FHIRPath calls: %v", trace.calls)
	}
}

func TestRetrieveMatchesStructuredCodesNotJSONSubstring(t *testing.T) {
	leak, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "8867-4-leak",
		"status": "final",
		"code": {"text": "Body weight"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	hit, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4", "display": "Heart rate"}], "text": "Heart rate"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{leak, hit},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), "[Observation: '8867-4'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("code match: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: 'Heart rate'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("display match: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: 'final'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("status must not match as terminology: %#v", got)
	}
}

func TestRetrieveDeclaredCodeAndValueSet(t *testing.T) {
	hit, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4", "display": "Heart rate"}]},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{hit},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseLibrary(`
library HeartLogic version '1.0.0'
using FHIR version '4.0.1'
codesystem "LOINC": 'http://loinc.org'
valueset "Heart Rate": 'http://example.org/ValueSet/heart-rate'
code "Heart rate": '8867-4' from "LOINC" display 'Heart rate'
context Patient
define "HR Count":
  [Observation: "Heart rate"].count()
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Codes) != 1 || lib.Codes[0].System != "http://loinc.org" || lib.Codes[0].Code != "8867-4" {
		t.Fatalf("code decl: %#v", lib.Codes)
	}
	if len(lib.ValueSets) != 1 || lib.ValueSets[0].URL != "http://example.org/ValueSet/heart-rate" {
		t.Fatalf("valueset decl: %#v", lib.ValueSets)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "HR Count", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("declared code retrieve: %#v", got)
	}
}

type staticMembership map[string]bool

func (s staticMembership) MemberOf(_ context.Context, valueSetURL, system, code string) (bool, error) {
	return s[valueSetURL+"|"+system+"|"+code], nil
}

func TestRetrieveValueSetMemberOf(t *testing.T) {
	hit, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4"}]},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	miss, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "wt",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "29463-7"}]},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{hit, miss},
		Terminology: staticMembership{
			"http://example.org/ValueSet/heart-rate|http://loinc.org|8867-4": true,
		},
		Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseLibrary(`
library HeartLogic version '1.0.0'
using FHIR version '4.0.1'
codesystem "LOINC": 'http://loinc.org'
valueset "Heart Rate": 'http://example.org/ValueSet/heart-rate'
code "HR": '8867-4' from "LOINC"
context Patient
define "HR Count":
  [Observation: "Heart Rate"].count()
define "Code In VS":
  "HR" in "Heart Rate"
`)
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "HR Count", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("valueset retrieve: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Code In VS", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("in valueset: %#v", got)
	}
}

func TestUnsupportedQueryAndInterval(t *testing.T) {
	eng := testEngine(t)
	_, err := eng.ParseExpression("from [Observation] O where O.status = 'final'")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported query, got %v", err)
	}
	_, err = eng.ParseExpression("Interval[1, 10]")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported Interval, got %v", err)
	}
	_, err = eng.ParseExpression("[Observation] O")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported related-context retrieve, got %v", err)
	}
}
