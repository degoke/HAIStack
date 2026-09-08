package fhirpath_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestEvalWithEnvExternalConstant(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	patientJSON := []byte(`{"resourceType":"Patient","id":"1","gender":"male"}`)
	env, err := types.NewJSONCodec().ParseJSON("Patient", patientJSON)
	if err != nil {
		t.Fatal(err)
	}
	values, err := engine.EvalWithEnv(context.Background(), "%patient.gender.exists()", env, map[string]any{
		"patient": env,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("expected one value, got %d", len(values))
	}
}
