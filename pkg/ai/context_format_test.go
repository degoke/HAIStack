package ai

import (
	"strings"
	"testing"
)

func TestJSONContextFormatter_Format(t *testing.T) {
	f := NewJSONContextFormatter()
	out, err := f.Format(map[string]any{"resourceType": "Patient", "id": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"resourceType"`) || !strings.Contains(out, "Patient") {
		t.Fatalf("json output: %q", out)
	}
}

func TestMarkdownContextFormatter_PatientResource(t *testing.T) {
	f := NewMarkdownContextFormatter()
	out, err := f.Format(map[string]any{
		"resourceType": "Patient",
		"id":           "pat-jane",
		"name": []any{
			map[string]any{"family": "Smith", "given": []any{"Jane"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# Patient") {
		t.Fatalf("expected title, got:\n%s", out)
	}
	if !strings.Contains(out, "**family:** Smith") {
		t.Fatalf("expected scalar field, got:\n%s", out)
	}
}

func TestMarkdownContextFormatter_SearchShape(t *testing.T) {
	f := NewMarkdownContextFormatter()
	out, err := f.Format(map[string]any{
		"resourceType": "Patient",
		"total":        1,
		"count":        1,
		"resources": []any{
			map[string]any{"resourceType": "Patient", "id": "p1", "gender": "female"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**total:** 1") {
		t.Fatalf("expected total, got:\n%s", out)
	}
	if !strings.Contains(out, "Patient (p1)") {
		t.Fatalf("expected resource bullet, got:\n%s", out)
	}
}

func TestNewToolContextFormatter(t *testing.T) {
	if _, ok := NewToolContextFormatter(ContextFormatJSON).(*ContextFormatter); !ok {
		t.Fatal("json kind should return ContextFormatter")
	}
	if _, ok := NewToolContextFormatter(ContextFormatMarkdown).(*MarkdownContextFormatter); !ok {
		t.Fatal("markdown kind should return MarkdownContextFormatter")
	}
	if _, ok := NewToolContextFormatter("unknown").(*ContextFormatter); !ok {
		t.Fatal("unknown kind should default to JSON")
	}
}

func TestToolContextFormatterInterface(t *testing.T) {
	var _ ToolContextFormatter = (*ContextFormatter)(nil)
	var _ ToolContextFormatter = (*MarkdownContextFormatter)(nil)
}
