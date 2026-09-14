package http_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/parquettest"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func newPatientSummaryRunHandler(t *testing.T) http.Handler {
	t.Helper()
	resources := storetest.NewResourceStore()
	if err := resources.Seed(context.Background(), fixtures.PatientJane(t), fixtures.PatientJohn(t)); err != nil {
		t.Fatalf("seed resources: %v", err)
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
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
