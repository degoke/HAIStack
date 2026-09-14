package http_test

import (
	"net/http"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/testkit/parquettest"
	"github.com/degoke/health-ai-stack/pkg/testkit/viewtest"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func newPatientSummaryRunHandler(t *testing.T) http.Handler {
	t.Helper()
	exec := viewtest.NewPatientSummaryExecutor(t, viewtest.DefaultPatientSummaryPatients(t))
	return viewOpsHandler(t, hahttp.Config{ViewRunService: view.NewRunService(exec)})
}

func TestViewDefinitionRunParquetQueryReturnsParquetBinary(t *testing.T) {
	h := newPatientSummaryRunHandler(t)
	rec := doRequest(t, h, http.MethodPost,
		"/fhir/$viewdefinition-run?viewName=patient_summary_view&_format=parquet",
		nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != view.ParquetContentType {
		t.Fatalf("Content-Type=%q, want %q", ct, view.ParquetContentType)
	}
	data := rec.Body.Bytes()
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet response body")
	}
	ids := parquettest.IDs(t, data)
	if len(ids) != 2 {
		t.Fatalf("parquet ids=%v, want both patients", ids)
	}
}
