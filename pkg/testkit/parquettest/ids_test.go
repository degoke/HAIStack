package parquettest_test

import (
	"bytes"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/parquettest"
	"github.com/degoke/health-ai-stack/pkg/view"
)

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
