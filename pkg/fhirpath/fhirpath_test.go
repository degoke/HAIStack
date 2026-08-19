package fhirpath_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	ppb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/patient_go_proto"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
)

func newEngine(t *testing.T, cfg fhirpath.Config) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func defaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	return newEngine(t, fhirpath.Config{})
}

func patientEnvelopeJSON() *types.ResourceEnvelope {
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"name": [{"given": ["Jane"], "family": "Doe"}],
		"telecom": [
			{"system": "phone", "value": "555-0100"},
			{"system": "email", "value": "jane@example.com"}
		]
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Patient", data)
	if err != nil {
		panic(err)
	}
	return env
}

func patientEnvelopeWithProto(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	codec := proto.NewGoogleR4Codec()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-1",
		"name": [{"given": ["Jane"], "family": "Doe"}],
		"telecom": [
			{"system": "phone", "value": "555-0100"},
			{"system": "email", "value": "jane@example.com"}
		]
	}`)
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	return env
}

func observationEnvelopeJSON() *types.ResourceEnvelope {
	data := []byte(`{
		"resourceType": "Observation",
		"id": "obs-1",
		"status": "final",
		"code": {"text": "Heart rate"},
		"valueQuantity": {"value": 72, "unit": "beats/min"}
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Observation", data)
	if err != nil {
		panic(err)
	}
	return env
}

func TestEval_MVPExamples(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	t.Run("Patient.name.given", func(t *testing.T) {
		values, err := eng.Eval(ctx, "Patient.name.given", env)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if len(values) != 1 {
			t.Fatalf("len(values) = %d, want 1", len(values))
		}
		s, err := values[0].String()
		if err != nil || s != "Jane" {
			t.Fatalf("String() = %q, %v; want Jane", s, err)
		}
	})

	t.Run("Patient.telecom.where(system = 'phone').value", func(t *testing.T) {
		values, err := eng.Eval(ctx, "Patient.telecom.where(system = 'phone').value", env)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if len(values) != 1 {
			t.Fatalf("len(values) = %d, want 1", len(values))
		}
		s, _ := values[0].String()
		if s != "555-0100" {
			t.Fatalf("value = %q, want 555-0100", s)
		}
	})

	t.Run("exists where first count comparisons", func(t *testing.T) {
		cases := []struct {
			expr string
			want int
		}{
			{"Patient.name.exists()", 1},
			{"Patient.name.where(family = 'Doe').count()", 1},
			{"Patient.name.first().family", 1},
			{"Patient.name.count() > 0", 1},
			{"Patient.active.exists() = false or Patient.name.exists()", 1},
		}
		for _, tc := range cases {
			values, err := eng.Eval(ctx, tc.expr, env)
			if err != nil {
				t.Errorf("%s: %v", tc.expr, err)
				continue
			}
			if len(values) != tc.want {
				t.Errorf("%s: len = %d, want %d", tc.expr, len(values), tc.want)
			}
		}
	})

	obs := observationEnvelopeJSON()
	codec := proto.NewGoogleR4Codec()
	obsProto, err := codec.ParseJSONToProto("Observation", obs.JSON)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	obsEnv := &types.ResourceEnvelope{ResourceType: "Observation", JSON: obs.JSON, Proto: obsProto}

	t.Run("Observation.value.ofType(Quantity).value", func(t *testing.T) {
		values, err := eng.Eval(ctx, "Observation.value.ofType(Quantity).value", obsEnv)
		if err != nil {
			t.Fatalf("Eval: %v", err)
		}
		if len(values) != 1 {
			t.Fatalf("len(values) = %d, want 1", len(values))
		}
		f, err := values[0].Float64()
		if err != nil || f != 72 {
			t.Fatalf("Float64() = %v, %v; want 72", f, err)
		}
	})
}

func TestEvalBool(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	ok, err := eng.EvalBool(ctx, "Patient.name.exists()", env)
	if err != nil || !ok {
		t.Fatalf("EvalBool exists: ok=%v err=%v", ok, err)
	}

	_, err = eng.EvalBool(ctx, "Patient.telecom.value", env)
	if !errors.Is(err, fhirpath.ErrNotSingleton) {
		t.Fatalf("EvalBool multi: err = %v, want ErrNotSingleton", err)
	}

	_, err = eng.EvalBool(ctx, "Patient.name.where(family = 'Missing')", env)
	if !errors.Is(err, fhirpath.ErrEmptyResult) {
		t.Fatalf("EvalBool empty: err = %v, want ErrEmptyResult", err)
	}

	_, err = eng.EvalBool(ctx, "Patient.name.given", env)
	if !errors.Is(err, fhirpath.ErrTypeMismatch) {
		t.Fatalf("EvalBool non-bool: err = %v, want ErrTypeMismatch", err)
	}
}

func TestEvalString(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	s, err := eng.EvalString(ctx, "Patient.name.given", env)
	if err != nil || s != "Jane" {
		t.Fatalf("EvalString: %q, %v", s, err)
	}

	_, err = eng.EvalString(ctx, "Patient.telecom.value", env)
	if !errors.Is(err, fhirpath.ErrNotSingleton) {
		t.Fatalf("EvalString multi: err = %v, want ErrNotSingleton", err)
	}

	_, err = eng.EvalString(ctx, "Patient.name.where(family = 'Missing').given", env)
	if !errors.Is(err, fhirpath.ErrEmptyResult) {
		t.Fatalf("EvalString empty: err = %v, want ErrEmptyResult", err)
	}

	_, err = eng.EvalString(ctx, "Patient.name.exists()", env)
	if !errors.Is(err, fhirpath.ErrTypeMismatch) {
		t.Fatalf("EvalString non-string: err = %v, want ErrTypeMismatch", err)
	}
}

func TestValueStringSupportsTemporalFHIRPathValues(t *testing.T) {
	cases := []struct {
		name  string
		value fhirpath.Value
		want  string
	}{
		{name: "date", value: fhirpath.NewValue(system.MustParseDate("2025-01-15")), want: "2025-01-15"},
		{name: "time", value: fhirpath.NewValue(system.MustParseTime("12:30:00")), want: "12:30:00"},
		{name: "datetime", value: fhirpath.NewValue(system.MustParseDateTime("2025-01-15T12:30:00Z")), want: "2025-01-15T12:30:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.value.String()
			if err != nil || got != tc.want {
				t.Fatalf("String()=%q err=%v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestEnvelopeUsesProtoWhenPresent(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)
	if env.Proto == nil {
		t.Fatal("expected proto on envelope")
	}

	s, err := eng.EvalString(ctx, "Patient.name.given", env)
	if err != nil || s != "Jane" {
		t.Fatalf("EvalString: %q, %v", s, err)
	}
}

func TestEnvelopeLazyJSONParse(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeJSON()
	if env.Proto != nil {
		t.Fatal("expected nil proto on JSON-only envelope")
	}

	s, err := eng.EvalString(ctx, "Patient.name.given", env)
	if err != nil || s != "Jane" {
		t.Fatalf("EvalString: %q, %v", s, err)
	}
}

func TestUnsupportedInput(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()

	_, err := eng.Eval(ctx, "Patient.name", map[string]any{"resourceType": "Patient"})
	if !errors.Is(err, fhirpath.ErrInvalidInput) {
		t.Fatalf("map input: err = %v, want ErrInvalidInput", err)
	}

	_, err = eng.Eval(ctx, "Patient.name", []byte(`{"resourceType":"Patient"}`))
	if !errors.Is(err, fhirpath.ErrInvalidInput) {
		t.Fatalf("raw JSON: err = %v, want ErrInvalidInput", err)
	}

	_, err = eng.Eval(ctx, "Patient.name", struct{}{})
	if !errors.Is(err, fhirpath.ErrInvalidInput) {
		t.Fatalf("struct input: err = %v, want ErrInvalidInput", err)
	}
}

func TestCompileCacheReusesExpression(t *testing.T) {
	eng := defaultEngine(t)
	c1, err := eng.Compile("Patient.name.given")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	c2, err := eng.Compile("Patient.name.given")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c1 != c2 {
		t.Fatal("expected cached compiled expression pointer reuse")
	}
}

func TestCustomFunction(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{
		Functions: map[string]fhirpath.Function{
			"customTrue": func(_ fhirpath.Collection, _ ...fhirpath.Collection) (fhirpath.Collection, error) {
				return fhirpath.Collection{fhirpath.NewValue(system.Boolean(true))}, nil
			},
		},
	})
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	ok, err := eng.EvalBool(ctx, "customTrue()", env)
	if err != nil || !ok {
		t.Fatalf("customTrue(): ok=%v err=%v", ok, err)
	}
}

func TestCustomFunctionWithArgs(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{
		Functions: map[string]fhirpath.Function{
			"echoPrefix": func(_ fhirpath.Collection, args ...fhirpath.Collection) (fhirpath.Collection, error) {
				if len(args) != 1 || len(args[0]) != 1 {
					return nil, errors.New("echoPrefix: expected one singleton arg collection")
				}
				s, err := args[0][0].String()
				if err != nil {
					return nil, err
				}
				return fhirpath.Collection{fhirpath.NewValue(system.String("prefix-" + s))}, nil
			},
		},
		FunctionArity: map[string]int{"echoPrefix": 1},
	})
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	s, err := eng.EvalString(ctx, "echoPrefix('hello')", env)
	if err != nil || s != "prefix-hello" {
		t.Fatalf("echoPrefix('hello'): %q, %v", s, err)
	}
}

func TestCustomFunctionArityRequired(t *testing.T) {
	_, err := fhirpath.NewEngine(fhirpath.Config{
		Functions: map[string]fhirpath.Function{
			"needsArg": func(fhirpath.Collection, ...fhirpath.Collection) (fhirpath.Collection, error) {
				return nil, nil
			},
		},
		FunctionArity: map[string]int{"needsArg": 9},
	})
	if err == nil || !strings.Contains(err.Error(), "arity 9 out of range") {
		t.Fatalf("NewEngine: err = %v, want arity range error", err)
	}
}

func TestCustomFunctionShadowsBuiltin(t *testing.T) {
	_, err := fhirpath.NewEngine(fhirpath.Config{
		Functions: map[string]fhirpath.Function{
			"where": func(input fhirpath.Collection, _ ...fhirpath.Collection) (fhirpath.Collection, error) {
				return input, nil
			},
		},
	})
	if !errors.Is(err, fhirpath.ErrShadowsBuiltin) {
		t.Fatalf("NewEngine: err = %v, want ErrShadowsBuiltin", err)
	}
}

func TestSoftTimeout(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{
		DefaultTimeout: 50 * time.Millisecond,
		Functions: map[string]fhirpath.Function{
			"slowFn": func(_ fhirpath.Collection, _ ...fhirpath.Collection) (fhirpath.Collection, error) {
				time.Sleep(200 * time.Millisecond)
				return fhirpath.Collection{fhirpath.NewValue(system.Boolean(true))}, nil
			},
		},
	})
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	_, err := eng.EvalBool(ctx, "slowFn()", env)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EvalBool timeout: err = %v, want DeadlineExceeded", err)
	}
}

func TestCallerContextDeadline(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{
		Functions: map[string]fhirpath.Function{
			"slowFn": func(_ fhirpath.Collection, _ ...fhirpath.Collection) (fhirpath.Collection, error) {
				time.Sleep(200 * time.Millisecond)
				return fhirpath.Collection{fhirpath.NewValue(system.Boolean(true))}, nil
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	env := patientEnvelopeWithProto(t)

	_, err := eng.EvalBool(ctx, "slowFn()", env)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EvalBool caller deadline: err = %v, want DeadlineExceeded", err)
	}
}

func TestMaxExpressionLen(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{MaxExpressionLen: 8})
	_, err := eng.Compile("Patient.name.given")
	if !errors.Is(err, fhirpath.ErrExpressionTooLong) {
		t.Fatalf("Compile: err = %v, want ErrExpressionTooLong", err)
	}
}

func TestMaxResultItems(t *testing.T) {
	eng := newEngine(t, fhirpath.Config{MaxResultItems: 1})
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	_, err := eng.Eval(ctx, "Patient.telecom.value", env)
	if !errors.Is(err, fhirpath.ErrTooManyResults) {
		t.Fatalf("Eval: err = %v, want ErrTooManyResults", err)
	}
}

func TestConcurrentCompileEval(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	env := patientEnvelopeWithProto(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := eng.EvalString(ctx, "Patient.name.given", env)
			if err != nil || s != "Jane" {
				t.Errorf("EvalString: %q, %v", s, err)
			}
		}()
	}
	wg.Wait()
}

func TestNotSupportedResolve(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	patient := &ppb.Patient{
		Id: &dtpb.Id{Value: "pat-1"},
		GeneralPractitioner: []*dtpb.Reference{{
			Reference: &dtpb.Reference_PatientId{PatientId: &dtpb.ReferenceId{Value: "other"}},
		}},
	}
	codec := proto.NewGoogleR4Codec()
	env, err := codec.ProtoToEnvelope("Patient", patient)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}

	_, err = eng.Eval(ctx, "Patient.generalPractitioner.resolve()", env)
	if err == nil || !errors.Is(err, fhirpath.ErrNotSupported) {
		t.Fatalf("resolve(): err = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Fatalf("error = %v, want resolve mention", err)
	}
}

func TestNotSupportedTerminology(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	obs := observationEnvelopeJSON()

	_, err := eng.Compile(`Observation.code.coding.memberOf('http://hl7.org/fhir/ValueSet/observation-codes')`)
	if err == nil || !errors.Is(err, fhirpath.ErrNotSupported) {
		t.Fatalf("Compile memberOf: err = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), "terminology") {
		t.Fatalf("error = %v, want terminology mention", err)
	}

	_, err = eng.Eval(ctx, `Observation.code.coding.memberOf('http://hl7.org/fhir/ValueSet/observation-codes')`, obs)
	if err == nil || !errors.Is(err, fhirpath.ErrNotSupported) {
		t.Fatalf("Eval memberOf: err = %v, want ErrNotSupported", err)
	}
}

func TestDirectProtoInput(t *testing.T) {
	eng := defaultEngine(t)
	ctx := context.Background()
	patient := &ppb.Patient{
		Id: &dtpb.Id{Value: "pat-1"},
		Name: []*dtpb.HumanName{{
			Given: []*dtpb.String{{Value: "Direct"}},
		}},
	}

	s, err := eng.EvalString(ctx, "Patient.name.given", patient)
	if err != nil || s != "Direct" {
		t.Fatalf("EvalString: %q, %v", s, err)
	}
}
