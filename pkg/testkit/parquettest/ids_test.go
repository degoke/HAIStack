package parquettest_test

import (
	"bytes"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/parquettest"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestStringColumnReadsNamedColumn(t *testing.T) {
	var buf bytes.Buffer
	result := &view.Result{
		ViewName: "patient_summary_view",
		Version:  "1.0.0",
		Columns: []view.ColumnInfo{
			{Name: "patient_id", Type: "string"},
		},
		Rows: []map[string]any{
			{"patient_id": "pat-jane"},
		},
	}
	if err := view.WriteParquetResult(&buf, result); err != nil {
		t.Fatalf("WriteParquetResult: %v", err)
	}
	ids := parquettest.StringColumn(t, buf.Bytes(), "patient_id")
	if len(ids) != 1 || ids[0] != "pat-jane" {
		t.Fatalf("StringColumn(patient_id)=%v", ids)
	}
}

func TestIDsReadsFlatViewParquet(t *testing.T) {
	var buf bytes.Buffer
	result := &view.Result{
		ViewName: "patient_summary_view",
		Version:  "1.0.0",
		Columns: []view.ColumnInfo{
			{Name: "id", Type: "string"},
		},
		Rows: []map[string]any{
			{"id": "pat-jane"},
			{"id": "pat-john"},
		},
	}
	if err := view.WriteParquetResult(&buf, result); err != nil {
		t.Fatalf("WriteParquetResult: %v", err)
	}
	ids := parquettest.IDs(t, buf.Bytes())
	if len(ids) != 2 || ids[0] != "pat-jane" || ids[1] != "pat-john" {
		t.Fatalf("ids=%v", ids)
	}
}
