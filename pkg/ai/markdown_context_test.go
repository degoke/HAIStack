package ai_test

import (
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestMarkdownContextBuilder_ViewTable(t *testing.T) {
	b := ai.NewMarkdownContextBuilder()
	md, err := b.FormatToolResult(ai.ToolRunView, map[string]any{
		"viewName": "patient_summary_view",
		"version":  "1.0.0",
		"columns":  []any{map[string]any{"name": "id"}, map[string]any{"name": "name"}},
		"rows": []map[string]any{
			{"id": "pat-jane", "name": "Jane Doe"},
		},
		"total": 1,
	}, []ai.Citation{{Kind: "view", Ref: "Patient/pat-jane"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "### View: patient_summary_view") {
		t.Fatalf("missing heading: %s", md)
	}
	if !strings.Contains(md, "| id |") || !strings.Contains(md, "pat-jane") {
		t.Fatalf("missing table: %s", md)
	}
}

func TestMarkdownContextBuilder_SearchList(t *testing.T) {
	b := ai.NewMarkdownContextBuilder()
	md, err := b.FormatToolResult(ai.ToolSearchFhirResources, map[string]any{
		"resourceType": "Patient",
		"total":        1,
		"resources": []map[string]any{
			{
				"id":   "pat-jane",
				"name": []map[string]any{{"given": []string{"Jane"}, "family": "Doe"}},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "Patient/pat-jane") {
		t.Fatalf("missing ref: %s", md)
	}
	if !strings.Contains(md, "Jane Doe") {
		t.Fatalf("missing name: %s", md)
	}
}
