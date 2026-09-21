package cql

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
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
	suffix, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"s","subject":{"reference":"Patient/not-ada"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resourceMatchesPatient(suffix, "Patient/ada") {
		t.Fatal("Patient/not-ada must not match Patient/ada")
	}
	canada, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"c","subject":{"reference":"Patient/canada"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resourceMatchesPatient(canada, "Patient/ada") {
		t.Fatal("Patient/canada must not match Patient/ada")
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

func TestDefineFunctionAndParameters(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library Fun version '1.0.0'
using FHIR version '4.0.1'
parameter "Threshold" Integer default 18
context Patient
define function "Plus"(a Integer, b Integer):
  a + b
define fluent function "isAdult"(age Integer):
  age >= Threshold
define "Sum":
  Plus(1, 2)
define "Adult":
  AgeInYears().isAdult()
`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "Sum", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("Plus: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Adult", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("fluent: %#v", got)
	}
}

func TestQueryIntervalQuantity(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"text": "Heart rate"},
		"subject": {"reference": "Patient/ada"},
		"effectiveDateTime": "2020-06-01"
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
	got, err := eng.Eval(context.Background(), "from [Observation] O where O.status = 'final' return O.id", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "hr" {
		t.Fatalf("query: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation] O where O.status = 'final'", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("aliased retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "5 in Interval[1, 10] and Interval[@2020-01-01, @2021-01-01) contains @2020-06-01", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("interval: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "2 'mg' + 3 'mg'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok := got[0].(Quantity)
	if !ok || q.Value != 5 || q.Unit != "mg" {
		t.Fatalf("quantity: %#v", got)
	}
}

func TestFHIRHelpersIncludeIndexerConvert(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library HelpersCheck version '1.0.0'
using FHIR version '4.0.1'
include FHIRHelpers version '4.0.1' called FHIRHelpers
context Patient
define "Given":
  Patient.name.given[0]
define "Converted":
  convert 5 to String
define "MadeDate":
  Date(2020, 6, 1)
define "Period":
  FHIRHelpers.ToInterval({ start: @2020-01-01, end: @2021-01-01 })
`)
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "Given", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("indexer: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Converted", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "5" {
		t.Fatalf("convert: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "MadeDate", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("date: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Period", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ToInterval: %#v", got)
	}
	if _, ok := asInterval(got[0]); !ok {
		t.Fatalf("ToInterval type: %#v", got[0])
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

func TestEvalUsesFHIRPathNestedName(t *testing.T) {
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stub := &nestedNameFHIRPath{inner: fp}
	eng, err := NewEngine(Config{
		FHIRPath: stub,
		Now:      func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "First(Patient.name.given)", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "FromFHIRPath" {
		t.Fatalf("expected FHIRPath HumanName conversion, got %#v", got)
	}
}

type nestedNameFHIRPath struct {
	inner fhirpath.Engine
}

func (t *nestedNameFHIRPath) Compile(expr string) (fhirpath.CompiledExpression, error) {
	return t.inner.Compile(expr)
}
func (t *nestedNameFHIRPath) Eval(ctx context.Context, expr string, resource any) ([]fhirpath.Value, error) {
	if expr == "name" {
		return []fhirpath.Value{fhirpath.NewValue(&dtpb.HumanName{
			Family: &dtpb.String{Value: "FromFP"},
			Given:  []*dtpb.String{{Value: "FromFHIRPath"}},
		})}, nil
	}
	return t.inner.Eval(ctx, expr, resource)
}
func (t *nestedNameFHIRPath) EvalWithEnv(ctx context.Context, expr string, resource any, env map[string]any) ([]fhirpath.Value, error) {
	return t.Eval(ctx, expr, resource)
}
func (t *nestedNameFHIRPath) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	return t.inner.EvalBool(ctx, expr, resource)
}
func (t *nestedNameFHIRPath) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	return t.inner.EvalString(ctx, expr, resource)
}

func TestEvalFHIRPathNestedNameConcurrent(t *testing.T) {
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		FHIRPath: fp,
		Now:      func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	pat := adaPatient(t)
	errCh := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := eng.Eval(context.Background(), "First(Patient.name.given)", EvalContext{Patient: pat})
			if err != nil {
				errCh <- err
				return
			}
			if len(got) != 1 || got[0] != "Ada" {
				errCh <- fmt.Errorf("given: %#v", got)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
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

func TestRetrieveIgnoresQuantityAndNotes(t *testing.T) {
	qty, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "wt",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "29463-7", "display": "Body weight"}]},
		"valueQuantity": {"value": 70, "unit": "kg", "system": "http://unitsofmeasure.org", "code": "kg"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	note, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "n",
		"status": "final",
		"code": {"text": "Note"},
		"note": [{"text": "Heart rate was recorded"}],
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{qty, note},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), "[Observation: 'kg'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("quantity unit must not match retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: 'http://unitsofmeasure.org'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("quantity system must not match retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: 'Heart rate was recorded'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("note text must not match retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: '29463-7'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("primary code should still match: %#v", got)
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

func TestInValueSetDoesNotFallbackToDisplayWhenTerminologySet(t *testing.T) {
	eng, err := NewEngine(Config{
		Terminology: staticMembership{},
		Now:         func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseLibrary(`
library HeartLogic version '1.0.0'
using FHIR version '4.0.1'
codesystem "LOINC": 'http://loinc.org'
valueset "Heart Rate": 'http://example.org/ValueSet/heart-rate'
code "Lookalike": '99999-9' from "LOINC" display 'Heart Rate'
context Patient
define "In VS":
  "Lookalike" in "Heart Rate"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "In VS", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("MemberOf false must not match valueset name/display: %#v", got)
	}
}

func TestQueryLetDoesNotLeakOrLoseToThis(t *testing.T) {
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
		Now:       func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseLibrary(`
library Lets version '1.0.0'
using FHIR version '4.0.1'
context Patient
define "Items":
  from {1, 2} X let y: X * 10 return y
define "Leaked":
  y
define "StatusLet":
  from [Observation] O let status: 'override' return status
`)
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "Items", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || fmt.Sprint(got[0]) != "10" || fmt.Sprint(got[1]) != "20" {
		t.Fatalf("let per row: %#v", got)
	}
	if _, err := eng.EvalDefine(context.Background(), lib, "Leaked", env); err == nil {
		t.Fatal("query let leaked into a later define")
	}
	got, err = eng.EvalDefine(context.Background(), lib, "StatusLet", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "override" {
		t.Fatalf("let should win over $this.status: %#v", got)
	}
}

func TestFHIRHelpersCalledAliasAndSecondInclude(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library HelpersAlias version '1.0.0'
using FHIR version '4.0.1'
include FHIRHelpers version '4.0.1' called FHIRHelpers
include FHIRHelpers version '4.0.1' called H
context Patient
define "ViaName":
  FHIRHelpers.ToInterval({ start: @2020-01-01, end: @2021-01-01 })
define "ViaAlias":
  H.ToInterval({ start: @2020-01-01, end: @2021-01-01 })
`)
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "ViaName", env)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := asInterval(got[0]); !ok {
		t.Fatalf("FHIRHelpers.ToInterval: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "ViaAlias", env)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := asInterval(got[0]); !ok {
		t.Fatalf("H.ToInterval: %#v", got)
	}
}

func TestIntervalOpenBoundsIncludes(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Interval(1, 10) includes Interval[1, 5]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("open outer must not include closed inner start: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Interval[1, 10] includes Interval[1, 5]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("closed outer includes closed inner: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Interval(1, 10) includes Interval(1, 5)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("open outer includes open inner at the same bound: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "1 during Interval(1, 10)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("point on open bound is not during: %#v", got)
	}
}

func TestRetrieveCodeEqualsAndEquivalent(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4", "display": "Heart rate"}], "text": "HR"},
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
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), `[Observation: code = "8867-4"]`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("code = code: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: code = "HR"]`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("code = text: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: code = "Heart rate"]`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("code = must not match display: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: code ~ "heart rate"]`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("code ~ display: %#v", got)
	}
}

func TestTimeUsesEngineClock(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Time(1, 2, 3)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := got[0].(time.Time)
	if !ok {
		t.Fatalf("Time(): %#v", got)
	}
	if tm.Year() != 2026 || tm.Month() != time.September || tm.Day() != 21 || tm.Hour() != 1 || tm.Minute() != 2 || tm.Second() != 3 {
		t.Fatalf("Time() used wall clock instead of engine Now: %v", tm)
	}
}

func TestQueryAggregateSortKeepsLetsAndNullReturn(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "from {1, 2} N let y: N * 10 aggregate t starting 0: t + y", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "30" {
		t.Fatalf("aggregate let: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "from {1, 2} N aggregate t starting 0: t + N", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "3" {
		t.Fatalf("aggregate source: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "from {2, 1} N let k: N return N sort by k", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || fmt.Sprint(got[0]) != "1" || fmt.Sprint(got[1]) != "2" {
		t.Fatalf("sort by let: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "from {1} A, {10, 20} B aggregate t starting 0: t + B", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "30" {
		t.Fatalf("aggregate second alias: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "from {1} X return null", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("null return row: %#v", got)
	}
	_, err = eng.ParseLibrary(`
library Bad version '1.0.0'
using FHIR version '4.0.1'
context Patient
define "Both":
  from {1} X return X aggregate t starting 0: t + X
`)
	if err == nil {
		t.Fatal("expected parse error for return and aggregate")
	}
}

func TestQueryDoesNotReadPatientWhenThisIsBound(t *testing.T) {
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
		Now:       func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "[Observation] where family = 'Lovelace'", EvalContext{Patient: adaPatient(t)})
	if err == nil && len(got) != 0 {
		t.Fatalf("Observation where family must not use Patient.family: %#v", got)
	}
}
