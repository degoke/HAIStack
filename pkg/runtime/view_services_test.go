package runtime_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestSQLiteRuntimeWiresViewRunWithoutAnalytics(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/view-run.db").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()

	if rt.Services().ViewRunService == nil {
		t.Fatal("expected ViewRunService on SQLite runtime")
	}
	if rt.Services().ViewExportService == nil {
		t.Fatal("expected ViewExportService on SQLite runtime")
	}

	req := httptest.NewRequest(http.MethodPost, "/fhir/$viewdefinition-run?viewName=patient_summary_view", nil)
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotImplemented {
		t.Fatalf("viewdefinition-run should be wired; body=%s", rec.Body.String())
	}
}
