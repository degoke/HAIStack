package ai

import (
	"context"
	"testing"
)

func TestFieldMatchesAllowedPolicy_PrefixChildren(t *testing.T) {
	allowed := []string{"name"}
	if !fieldMatchesAllowedPolicy("name[0].family", allowed) {
		t.Fatal("expected name[0].family under name")
	}
	if !fieldMatchesAllowedPolicy("name.family", allowed) {
		t.Fatal("expected name.family under name")
	}
	if fieldMatchesAllowedPolicy("nameplate", allowed) {
		t.Fatal("nameplate must not match name prefix")
	}
}

func TestAllowListPolicy_UpdateAllowsChildPatchPath(t *testing.T) {
	policy := NewAllowListPolicy()
	policy.Write["Patient"] = WriteTypePolicy{UpdateFields: []string{"name"}}
	decision, err := policy.CheckWrite(context.Background(), WritePolicyRequest{
		Operation:    WriteOperationUpdate,
		ResourceType: "Patient",
		Fields:       map[string]any{"name[0].family": "X"},
	})
	if err != nil || decision == nil || !decision.Allowed {
		t.Fatalf("decision = %#v err=%v", decision, err)
	}
}
