package ai_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestFHIRDeidentifier_SearchFromJSONBytes(t *testing.T) {
	deid, err := ai.NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"resourceType":"Patient","resources":[{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}]}`)
	out, redactions, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName:     ai.ToolSearchFhirResources,
		ResourceType: "Patient",
		Data:         raw,
	})
	if err != nil {
		t.Fatalf("Deidentify: %v", err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
	m := out.(map[string]any)
	res := m["resources"].([]any)[0].(map[string]any)
	if res["name"] != ai.DefaultRedactedValue {
		t.Fatalf("name = %v", res["name"])
	}
	b, _ := json.Marshal(m)
	if !json.Valid(b) {
		t.Fatal("invalid JSON result")
	}
}
