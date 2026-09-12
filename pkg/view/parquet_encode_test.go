package view_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestWriteParquetResultProducesBinaryParquet(t *testing.T) {
	result := &view.Result{
		ViewName: "patient_summary_view",
		Version:  "1.0.0",
		Columns: []view.ColumnInfo{
			{Name: "patient_id", Type: "string"},
			{Name: "active", Type: "boolean"},
		},
		Rows: []map[string]any{
			{"patient_id": "p1", "active": true},
			{"patient_id": "p2", "active": false},
		},
	}
	var buf bytes.Buffer
	if err := view.WriteParquetResult(&buf, result); err != nil {
		t.Fatalf("WriteParquetResult: %v", err)
	}
	data := buf.Bytes()
	if !view.IsParquetFile(data) {
		t.Fatalf("expected PAR1 magic, got %q", string(data[:min(4, len(data))]))
	}
	rows, err := view.ParquetFileRowCount(data)
	if err != nil {
		t.Fatalf("ParquetFileRowCount: %v", err)
	}
	if rows != 2 {
		t.Fatalf("rows=%d, want 2", rows)
	}
}

func TestWriteParquetExportStreamsPages(t *testing.T) {
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}

	var buf bytes.Buffer
	rowCount, err := view.WriteParquetExport(context.Background(), &buf, exec, view.ExecuteRequest{
		ViewName: "patient_summary_view",
		Version:  "1.0.0",
	}, 1)
	if err != nil {
		t.Fatalf("WriteParquetExport: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("rowCount=%d, want 1", rowCount)
	}
	if !view.IsParquetFile(buf.Bytes()) {
		t.Fatal("expected parquet output")
	}
}

func TestEncodeRunResultParquetContentType(t *testing.T) {
	result := &view.Result{
		Columns: []view.ColumnInfo{{Name: "id", Type: "string"}},
		Rows:    []map[string]any{{"id": "p1"}},
	}
	_, contentType, err := view.EncodeRunResultForTest(result, view.FormatParquet, false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if contentType != view.ParquetContentType {
		t.Fatalf("contentType=%q", contentType)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
