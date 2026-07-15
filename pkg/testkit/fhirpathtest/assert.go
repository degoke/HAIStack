package fhirpathtest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// DefaultEngine returns a FHIRPath engine for tests.
func DefaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("fhirpathtest.DefaultEngine: %v", err)
	}
	return eng
}

// Eval evaluates expr against env and returns stringified values.
func Eval(t *testing.T, eng fhirpath.Engine, env *types.ResourceEnvelope, expr string) []string {
	t.Helper()
	ctx := context.Background()
	var resource any = env
	if env.Proto != nil {
		resource = env.Proto
	}
	values, err := eng.Eval(ctx, expr, resource)
	if err != nil {
		t.Fatalf("Eval %q: %v", expr, err)
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, err := v.String(); err == nil {
			out = append(out, s)
			continue
		}
		out = append(out, fmt.Sprint(v.Raw()))
	}
	return out
}

// AssertValues asserts the expression result matches want values (order-sensitive).
func AssertValues(t *testing.T, eng fhirpath.Engine, env *types.ResourceEnvelope, expr string, want ...string) {
	t.Helper()
	got := Eval(t, eng, env, expr)
	if len(got) != len(want) {
		t.Fatalf("Eval %q = %v, want %v", expr, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Eval %q[%d] = %q, want %q (full: %v)", expr, i, got[i], want[i], got)
		}
	}
}

// AssertEmpty asserts the expression evaluates to an empty collection.
func AssertEmpty(t *testing.T, eng fhirpath.Engine, env *types.ResourceEnvelope, expr string) {
	t.Helper()
	got := Eval(t, eng, env, expr)
	if len(got) != 0 {
		t.Fatalf("Eval %q = %v, want empty", expr, got)
	}
}

// AssertContains asserts at least one result value equals want.
func AssertContains(t *testing.T, eng fhirpath.Engine, env *types.ResourceEnvelope, expr, want string) {
	t.Helper()
	got := Eval(t, eng, env, expr)
	for _, v := range got {
		if v == want {
			return
		}
	}
	t.Fatalf("Eval %q = %v, want to contain %q", expr, got, want)
}

// AssertString asserts EvalString matches want.
func AssertString(t *testing.T, eng fhirpath.Engine, env *types.ResourceEnvelope, expr, want string) {
	t.Helper()
	ctx := context.Background()
	resource := any(env)
	if env.Proto != nil {
		resource = env.Proto
	}
	got, err := eng.EvalString(ctx, expr, resource)
	if err != nil {
		t.Fatalf("EvalString %q: %v", expr, err)
	}
	if strings.TrimSpace(got) != want {
		t.Fatalf("EvalString %q = %q, want %q", expr, got, want)
	}
}
