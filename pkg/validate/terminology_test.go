package validate_test

import (
	"context"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"testing"
)

func TestTerminologyValidationIsOptInAndDisplayIsWarning(t *testing.T) {
	ctx := context.Background()
	st := terminology.NewMemoryStore()
	raw := []byte(`{"resourceType":"CodeSystem","url":"urn:codes","version":"1","concept":[{"code":"ok","display":"Okay"}]}`)
	if err := st.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "CodeSystem", CanonicalURL: "urn:codes", Version: "1", Status: "active", ResourceJSON: raw}); err != nil {
		t.Fatal(err)
	}
	if err := terminology.Compile(ctx, st, "s", "", raw); err != nil {
		t.Fatal(err)
	}
	eng, err := validate.NewEngine(validate.Config{})
	if err != nil {
		t.Fatal(err)
	}
	env := &types.ResourceEnvelope{ResourceType: "Observation", JSON: []byte(`{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"urn:codes","code":"ok","display":"Wrong"}]}}`)}
	off, err := eng.Validate(ctx, env, validate.ValidateOptions{})
	if err != nil || !off.Valid {
		t.Fatalf("off=%+v err=%v", off, err)
	}
	on, err := eng.Validate(ctx, env, validate.ValidateOptions{Terminology: &terminology.LocalService{Store: st, ScopeID: "s"}, TerminologyEnabled: true, TerminologyBindings: map[string]validate.TerminologyBinding{"code": {URL: "urn:codes", Version: "1", Strength: "required"}}})
	if err != nil || !on.Valid || len(on.Issues) != 1 || on.Issues[0].Severity != "warning" {
		t.Fatalf("on=%+v err=%v", on, err)
	}
}
