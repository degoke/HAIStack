package ai_test

import (
	"context"
	"encoding/json"
	"strings"
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

func TestSearchJSONScrubRejectsNonObjectResource(t *testing.T) {
	deid, err := ai.NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"resourceType":"Bundle","resources":["not-an-object"]}`)
	_, _, err = deid.Deidentify(context.Background(), ai.DeidentifyRequest{
		ToolName:     ai.ToolSearchFhirResources,
		ResourceType: "Patient",
		Data:         raw,
	})
	if err == nil {
		t.Fatal("expected error for non-object resources element")
	}
}

func TestSearchJSONScrubMinifiedAndPretty(t *testing.T) {
	deid, err := ai.NewFHIRDeidentifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"name":         []map[string]any{{"family": "Doe"}},
	}
	included := map[string]any{
		"resourceType": "Organization",
		"id":           "org-1",
		"name":         "Acme Clinic",
	}
	envelope := map[string]any{
		"resourceType": "Bundle",
		"type":         "searchset",
		"resources":    []any{inner},
		"included":     []any{included},
	}
	minified, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var prettyBuf strings.Builder
	enc := json.NewEncoder(&prettyBuf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		t.Fatal(err)
	}
	pretty := []byte(strings.TrimSpace(prettyBuf.String()))

	for _, label := range []string{"minified", "pretty"} {
		raw := minified
		if label == "pretty" {
			raw = pretty
		}
		if !json.Valid(raw) {
			t.Fatalf("%s: invalid input JSON", label)
		}
		out, redactions, err := deid.Deidentify(context.Background(), ai.DeidentifyRequest{
			ToolName:     ai.ToolSearchFhirResources,
			ResourceType: "Patient",
			Data:         raw,
		})
		if err != nil {
			t.Fatalf("%s: Deidentify: %v", label, err)
		}
		if len(redactions) == 0 {
			t.Fatalf("%s: expected redactions", label)
		}
		m := out.(map[string]any)
		pat := m["resources"].([]any)[0].(map[string]any)
		if pat["name"] != ai.DefaultRedactedValue {
			t.Fatalf("%s: patient name = %v", label, pat["name"])
		}
		org := m["included"].([]any)[0].(map[string]any)
		if org["name"] != ai.DefaultRedactedValue {
			t.Fatalf("%s: org name = %v", label, org["name"])
		}
		round, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("%s: marshal: %v", label, err)
		}
		if !json.Valid(round) {
			t.Fatalf("%s: invalid JSON after scrub", label)
		}
	}
}
