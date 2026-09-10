package runtime_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestSQLiteRuntimePersistsViewExportJobMetadata(t *testing.T) {
	dataDir := t.TempDir()
	rt, err := runtime.New().
		WithSQLite(filepath.Join(dataDir, "view-run.db")).
		WithDataDir(dataDir).
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()

	req := httptest.NewRequest(http.MethodPost, "/fhir/$viewdefinition-export?viewName=patient_summary_view&_outputFormat=ndjson", nil)
	req.Header.Set("Prefer", "respond-async")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("export kickoff status=%d body=%s", rec.Code, rec.Body.String())
	}

	statusURL := rec.Header().Get("Content-Location")
	if statusURL == "" {
		t.Fatalf("missing Content-Location header body=%s", rec.Body.Bytes())
	}
	jobID := filepath.Base(statusURL)
	if idx := strings.LastIndex(jobID, "/"); idx >= 0 {
		jobID = jobID[idx+1:]
	}

	jobPath := filepath.Join(dataDir, "jobs", "view-export", jobID+".json")
	if _, err := os.Stat(jobPath); err != nil {
		t.Fatalf("expected durable export job metadata at %s: %v", jobPath, err)
	}
}
