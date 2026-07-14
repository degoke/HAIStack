package analytics_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestCSVSink_HeaderOrderingAndEncoding(t *testing.T) {
	ctx := context.Background()
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Columns: []view.ColumnInfo{
			{Name: "id", Type: "string"},
			{Name: "given", Type: "string"},
			{Name: "tags", Type: "string"},
			{Name: "score", Type: "decimal"},
			{Name: "active", Type: "boolean"},
			{Name: "missing", Type: "string"},
		},
		Rows: []map[string]any{
			{
				"id":      "pat-1",
				"given":   "Jane",
				"tags":    []any{"a", "b"},
				"score":   72.5,
				"active":  true,
				"missing": nil,
			},
		},
	}

	var buf bytes.Buffer
	sink := analytics.NewCSVSink(&buf)
	if err := sink.WriteRows(ctx, result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0] != "id,given,tags,score,active,missing" {
		t.Fatalf("header = %q", lines[0])
	}
	if lines[1] != `pat-1,Jane,"[""a"",""b""]",72.5,true,` {
		t.Fatalf("row = %q", lines[1])
	}
}

func TestCSVSink_EmptyResultSet(t *testing.T) {
	ctx := context.Background()
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Columns: []view.ColumnInfo{
			{Name: "id", Type: "string"},
			{Name: "given", Type: "string"},
		},
		Rows: nil,
	}

	var buf bytes.Buffer
	if err := analytics.NewCSVSink(&buf).WriteRows(ctx, result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if buf.String() != "id,given\n" {
		t.Fatalf("csv = %q, want header only", buf.String())
	}
}
