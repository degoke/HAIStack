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

func TestEmptyELMLibraryIsUnsupported(t *testing.T) {
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
		t.Fatalf("expected unsupported empty ELM library, got %v", err)
	}
	eng := testEngine(t)
	_, err = eng.CompileLibrary(env)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported empty ELM compile, got %v", err)
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
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("implicit retrieve must not match display: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: code ~ 'Heart rate'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("code ~ display: %#v", got)
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
	got, err = eng.Eval(context.Background(), "[Observation: code = 'http://unitsofmeasure.org'].count()", env)
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
	if len(got) != 0 {
		t.Fatalf("code = must not match text: %#v", got)
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

func TestUnionIsDistinct(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{1, 2} union {2, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || fmt.Sprint(got) != "[1 2 3]" {
		t.Fatalf("union must be distinct: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, 1} | {1, 2}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || fmt.Sprint(got) != "[1 2]" {
		t.Fatalf("list union must be distinct: %#v", got)
	}
}

func TestRetrieveValueSetRequiresTerminology(t *testing.T) {
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
valueset "Heart Rate": 'http://example.org/ValueSet/heart-rate'
context Patient
define "HR Count":
  [Observation: "Heart Rate"].count()
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.EvalDefine(context.Background(), lib, "HR Count", EvalContext{Patient: adaPatient(t)})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("declared valueset retrieve without terminology: %v", err)
	}
	_, err = eng.Eval(context.Background(), "[Observation: 'http://example.org/ValueSet/heart-rate']", EvalContext{Patient: adaPatient(t)})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("canonical valueset retrieve without terminology: %v", err)
	}
}

func TestRetrieveMatchesCategoryAndRelatedFields(t *testing.T) {
	cat, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4"}]},
		"category": [{"coding": [{"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "vital-signs"}]}],
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	comp, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "panel",
		"status": "final",
		"code": {"text": "Panel"},
		"component": [{"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4"}]}}],
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{cat, comp},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), "[Observation: category in 'vital-signs'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("category retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: '8867-4'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("component code must not match retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: category in '8867-4'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("category retrieve must not fall back to code: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: Category in 'vital-signs'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("category retrieve is case-insensitive: %#v", got)
	}
}

func TestQueryAggregateThenSort(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "from { {v: 3}, {v: 1}, {v: 2}, {v: 1} } X aggregate t starting {}: t union {X} sort by v", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("aggregate then sort: %#v", got)
	}
	v0, _ := got[0].(map[string]any)
	v1, _ := got[1].(map[string]any)
	v2, _ := got[2].(map[string]any)
	if fmt.Sprint(v0["v"]) != "1" || fmt.Sprint(v1["v"]) != "2" || fmt.Sprint(v2["v"]) != "3" {
		t.Fatalf("aggregate then sort: %#v", got)
	}
}

func TestEvalMemberKeepsNullForMissingFields(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{ {id: 1, v: 10}, {id: 2} }.v", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || fmt.Sprint(got[0]) != "10" || got[1] != nil {
		t.Fatalf("missing member must stay null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{ {id: 1, v: 10}, {id: 2} }.v.count()", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("Count must ignore null members: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "exists { {id: 2} }.v", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("exists must ignore null members: %#v", got)
	}
}

func TestIntervalUnionExceptAndExpand(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Interval[1, 5] union Interval[3, 10]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	iv, ok := asInterval(got[0])
	if !ok || fmt.Sprint(iv.Low) != "1" || fmt.Sprint(iv.High) != "10" {
		t.Fatalf("interval union: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Interval[1, 5] union Interval[10, 12]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("non-overlapping interval union should be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Interval[1, 10] except Interval[8, 15]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	iv, ok = asInterval(got[0])
	if !ok || fmt.Sprint(iv.Low) != "1" || fmt.Sprint(iv.High) != "8" || iv.HighClosed {
		t.Fatalf("interval except: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, 1, 2} except {2}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "1" {
		t.Fatalf("list except must be distinct: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "expand Interval[1, 3]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expand: %#v", got)
	}
}

func TestSumAvgSkipNullsAndChoiceValue(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Sum(from {1, 2, 3} X return if X = 2 then null else X)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "4" {
		t.Fatalf("Sum should skip nulls: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Avg(from {2, 4, 6} X return if X = 4 then null else X)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || fmt.Sprint(got[0]) != "4" {
		t.Fatalf("Avg should skip nulls: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{ valueQuantity: 1 'mg', valueCodeableConcept: 'x' }.value", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("choice .value must be deterministic: %#v", got)
	}
}

func TestUnionIdentifiesFHIRResources(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"text": "Heart rate"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	dup, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "amended",
		"code": {"text": "Heart rate"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cqlEqual(obs, dup) {
		t.Fatal("same resourceType/id must be equal")
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{obs, dup},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "[Observation] union [Observation]", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("resource union must distinct by type/id: %#v", got)
	}
}

func TestSingletonUnionDoesNotTreatPeriodMapsAsIntervals(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), `{ {resourceType: 'Appointment', id: 'a1', start: @2020-01-01, end: @2020-01-02} } union { {resourceType: 'Appointment', id: 'a1', start: @2021-01-01, end: @2021-01-02} }`, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("appointment union must distinct by id, not merge intervals: %#v", got)
	}
	obj, ok := got[0].(map[string]any)
	if !ok || obj["id"] != "a1" {
		t.Fatalf("expected appointment map, got %#v", got)
	}
	got, err = eng.Eval(context.Background(), `{ {start: @2020-01-01, end: @2020-06-01} } union Interval[@2020-07-01, @2020-12-31]`, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Period map union Interval must stay a list, got %#v", got)
	}
}

func TestRetrieveValueChoiceAndEncounterClass(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "pos",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4"}]},
		"valueCodeableConcept": {"coding": [{"code": "positive"}]},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := types.NewJSONCodec().ParseJSON("Encounter", []byte(`{
		"resourceType": "Encounter",
		"id": "e1",
		"status": "finished",
		"class": {"system": "http://terminology.hl7.org/CodeSystem/v3-ActCode", "code": "IMP"},
		"type": [{"coding": [{"code": "office"}]}],
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{obs, enc},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), "[Observation: value in 'positive'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("value[x] retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Encounter: 'IMP'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("implicit retrieve must not match Encounter.class: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Encounter: class in 'IMP'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("named class retrieve: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: status = '8867-4'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("named status retrieve must not fall back to Observation.code: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "[Observation: category in '8867-4'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("missing category must not fall back to Observation.code: %#v", got)
	}
}

func TestCQLDoesNotCoerceStringsAndSkipsAllTrueNulls(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "'1' = 1", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("'1' = 1 must be false: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "'true' and true", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("'true' and true must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "1 > 'a'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("incomparable compare must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "AllTrue(from {1, 2} X return if X = 2 then null else true)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("AllTrue must skip nulls: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{ {id: 1, v: null} }.v", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("present JSON null must stay null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{ system: 'http://loinc.org', code: '8867-4' } ~ { system: 'http://loinc.org', code: '8867-4', display: 'Heart rate' }", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("code ~ must use system/code, not Sprint: %#v", got)
	}
}

func TestAgeInYearsAndTodayUseClockZone(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	pat, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
		"resourceType": "Patient", "id": "ada", "birthDate": "2000-01-02"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Now: func() time.Time { return time.Date(2021, 1, 1, 23, 59, 59, 0, loc) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "AgeInYears()", EvalContext{Patient: pat, Now: time.Date(2021, 1, 1, 23, 59, 59, 0, loc)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(20) {
		t.Fatalf("AgeInYears must use local period-end calendar: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Today()", EvalContext{Now: time.Date(2021, 1, 1, 23, 59, 59, 0, loc)})
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := got[0].(time.Time)
	if !ok || tm.Day() != 1 || tm.Month() != time.January || tm.Year() != 2021 {
		t.Fatalf("Today() must use clock zone date: %#v", got)
	}
}

func TestFlattenJSONKeepsDateShapedStrings(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "vs",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4", "display": "Heart Rate"}]},
		"valueString": "2024",
		"effectiveDateTime": "2021-01-01",
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
	got, err := eng.Eval(context.Background(), "[Observation].value = '2024'", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("valueString 2024 must stay a string: %#v", got)
	}
	loc := time.FixedZone("EST", -5*3600)
	got, err = eng.Eval(context.Background(), `[Observation].effective during "Measurement Period"`, EvalContext{
		Patient: adaPatient(t),
		Parameters: map[string]any{
			"Measurement Period": Interval{
				Low:        time.Date(2021, 1, 1, 0, 0, 0, 0, loc),
				High:       time.Date(2021, 1, 1, 23, 59, 59, 999999999, loc),
				LowClosed:  true,
				HighClosed: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("date-only effectiveDateTime must be during EST period: %#v", got)
	}
}

func TestPeriodMapIsNotEqualToInterval(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), `{ start: @2020-01-01, end: @2020-12-31 } = Interval[@2020-01-01, @2020-12-31]`, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("Period map must not equal Interval: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `{ {id: 'a', start: @2020-01-01, end: @2020-12-31}, {id: 'b', start: @2020-01-01, end: @2020-12-31} }.distinct().count()`, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("distinct Period maps must keep different ids: %#v", got)
	}
}

func TestUnresolvedRetrieveDoesNotMatchDisplay(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4", "display": "Heart Rate"}]},
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
	got, err := eng.Eval(context.Background(), `[Observation: "Heart Rate"].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("unresolved valueset name must not match display: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: '8867-4'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("unresolved code string must still match code: %#v", got)
	}
}

func TestListLiteralsKeepNull(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{null}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("{null} must keep a null element: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, null, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != int64(1) || got[1] != nil || got[2] != int64(3) {
		t.Fatalf("{1, null, 3} must keep null: %#v", got)
	}
}

func TestConcatRequiresStrings(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "1 + 'x'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("1 + 'x' must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "1 & 'x'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("1 & 'x' must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "null & 'x'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("null & 'x' must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "'a' & 'b'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "ab" {
		t.Fatalf("string &: %#v", got)
	}
}

func TestTimeUsesClockZone(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	eng, err := NewEngine(Config{
		Now: func() time.Time { return time.Date(2021, 1, 1, 23, 59, 59, 0, loc) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "Time(1, 2, 3)", EvalContext{Now: time.Date(2021, 1, 1, 23, 59, 59, 0, loc)})
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := got[0].(time.Time)
	if !ok || tm.Year() != 2021 || tm.Month() != time.January || tm.Day() != 1 || tm.Hour() != 1 {
		t.Fatalf("Time() must stay on local clock date: %v", got)
	}
}

func TestRetrieveMedicationReference(t *testing.T) {
	med, err := types.NewJSONCodec().ParseJSON("Medication", []byte(`{
		"resourceType": "Medication",
		"id": "asa",
		"code": {"coding": [{"system": "http://www.nlm.nih.gov/research/umls/rxnorm", "code": "1191"}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	req, err := types.NewJSONCodec().ParseJSON("MedicationRequest", []byte(`{
		"resourceType": "MedicationRequest",
		"id": "mr1",
		"status": "active",
		"intent": "order",
		"medicationReference": {"reference": "Medication/asa"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{med, req},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t)}
	got, err := eng.Eval(context.Background(), "[MedicationRequest: '1191'].count()", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("medicationReference retrieve: %#v", got)
	}
}

func TestNumericCodeDoesNotInventMatch(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "n",
		"status": "final",
		"code": {"coding": [{"code": 88674}]},
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
	got, err := eng.Eval(context.Background(), "[Observation: '88674'].count()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("numeric JSON code must not Sprint-match: %#v", got)
	}
}

func TestRetrieveCodeCaseIsExact(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"coding": [{"system": "http://loinc.org", "code": "8867-4"}]},
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
	got, err := eng.Eval(context.Background(), `[Observation: '8867-4'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("exact code: %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: '8867-4'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	got, err = eng.Eval(context.Background(), `[Observation: code = '8867-4'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("code = : %#v", got)
	}
	got, err = eng.Eval(context.Background(), `[Observation: '8867-X'].count()`, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("implicit miss: %#v", got)
	}
}

func TestListLiteralsDoNotFlatten(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{{1, 2}}.count()", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("{{1, 2}} count: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, {2, 3}}.count()", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("{1, {2, 3}} count: %#v", got)
	}
	obs1, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"a","status":"final","code":{"text":"A"},"subject":{"reference":"Patient/ada"}}`))
	if err != nil {
		t.Fatal(err)
	}
	obs2, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{"resourceType":"Observation","id":"b","status":"final","code":{"text":"B"},"subject":{"reference":"Patient/ada"}}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err = NewEngine(Config{
		Retriever: StaticRetriever{obs1, obs2},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = eng.Eval(context.Background(), "{[Observation]}.count()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("{[Observation]} must wrap the retrieve: %#v", got)
	}
}

func TestStringDoesNotCompareAsDate(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "'2024' > @2020-01-01", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("string vs Date compare must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "'2021-01-01' during Interval[@2021-01-01T00:00:00-05:00, @2021-01-01T23:59:59-05:00]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("string during Interval must be null: %#v", got)
	}
}

func TestDateEqualsZonedMidnight(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "@2021-01-01 = @2021-01-01T00:00:00-05:00", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("date-only = zoned midnight: %#v", got)
	}
}

func TestNaiveDateTimeUsesCompareZone(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "n",
		"status": "final",
		"code": {"text": "N"},
		"effectiveDateTime": "2021-01-02T03:00:00",
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
	loc := time.FixedZone("EST", -5*3600)
	got, err := eng.Eval(context.Background(), `[Observation].effective during "Measurement Period"`, EvalContext{
		Patient: adaPatient(t),
		Parameters: map[string]any{
			"Measurement Period": Interval{
				Low:        time.Date(2021, 1, 1, 0, 0, 0, 0, loc),
				High:       time.Date(2021, 1, 1, 23, 59, 59, 999999999, loc),
				LowClosed:  true,
				HighClosed: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("naive Jan 2 03:00 must not be during EST Jan 1: %#v", got)
	}
}

func TestValuePathDoesNotFollowValueReference(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "o",
		"status": "final",
		"code": {"text": "A"},
		"valueReference": {"reference": "Condition/c1"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cond, err := types.NewJSONCodec().ParseJSON("Condition", []byte(`{
		"resourceType": "Condition",
		"id": "c1",
		"code": {"coding": [{"code": "E11"}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{obs, cond},
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "[Observation: value in 'E11'].count()", EvalContext{Patient: adaPatient(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(0) {
		t.Fatalf("value path must not follow valueReference: %#v", got)
	}
}

func TestStartsWithRequiresStrings(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "'1'.startsWith(1)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("startsWith mixed types must be null: %#v", got)
	}
}

func TestListEqualityComparesAllElements(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{1, 2} = {1, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("{1, 2} = {1, 3} must be false: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, 2} = {1, 2}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("{1, 2} = {1, 2}: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, 2} != {1, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("{1, 2} != {1, 3}: %#v", got)
	}
}

func TestNestedListSurvivesMemberAccess(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{ x: {{1, 2}} }.x.count()", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("tuple nested list count: %#v", got)
	}
}

func TestFirstFunctionUnwrapsNestedList(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "First({{1, 2}}).count()", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("First({{1, 2}}).count() must be 2: %#v", got)
	}
}

func TestDateTimeConstructorUsesCompareZone(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "DateTime(2021, 1, 1, 3, 0, 0) during Interval[@2021-01-01T00:00:00-05:00, @2021-01-01T23:59:59-05:00]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("naive DateTime during EST day: %#v", got)
	}
}

func TestReplaceRequiresStrings(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "'abc'.replace(1, 'x')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("replace mixed types must be null: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "'abc'.replace('b', 'x')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "axc" {
		t.Fatalf("replace strings: %#v", got)
	}
}
