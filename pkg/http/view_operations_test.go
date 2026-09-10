package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/view"
)

type fakeViewRunService struct {
	last view.ViewRunRequest
}

func (f *fakeViewRunService) Execute(_ context.Context, req view.ViewRunRequest) ([]byte, string, error) {
	f.last = req
	return []byte(`{"rows":[]}`), "application/json", nil
}

type fakeSQLQueryService struct {
	last view.SQLQueryRequest
}

func (f *fakeSQLQueryService) Execute(_ context.Context, req view.SQLQueryRequest) ([]byte, string, error) {
	f.last = req
	return []byte(`{"rows":[]}`), "application/json", nil
}

type fakeViewExportService struct {
	jobs  map[string]*view.ViewExportJob
	files map[string][]byte
}

func newFakeViewExportService() *fakeViewExportService {
	return &fakeViewExportService{
		jobs:  make(map[string]*view.ViewExportJob),
		files: make(map[string][]byte),
	}
}

func (f *fakeViewExportService) Kickoff(_ context.Context, req view.ViewExportRequest) (*view.ViewExportJob, error) {
	job := &view.ViewExportJob{
		ID:     "export-job-1",
		Status: view.ExportComplete,
		Request: req,
		Files: []view.ExportFile{{
			ViewName: "patient_summary_view",
			Filename: "patient_summary_view-1.0.0.ndjson",
		}},
	}
	f.jobs[job.ID] = job
	f.files[job.ID+"/patient_summary_view-1.0.0.ndjson"] = []byte(`{"id":"p1"}`)
	return job, nil
}

func (f *fakeViewExportService) GetJob(_ context.Context, jobID string) (*view.ViewExportJob, error) {
	job, ok := f.jobs[jobID]
	if !ok {
		return nil, errNotFound("job")
	}
	return job, nil
}

func (f *fakeViewExportService) Cancel(context.Context, string) error { return nil }

func (f *fakeViewExportService) StatusURL(jobID string) string {
	return "/fhir/ViewDefinition/$viewdefinition-export/status/" + jobID
}

func (f *fakeViewExportService) FileURL(jobID, filename string) string {
	return "/fhir/ViewDefinition/$viewdefinition-export/files/" + jobID + "/" + filename
}

func (f *fakeViewExportService) GetFile(_ context.Context, jobID, filename string) ([]byte, string, error) {
	data, ok := f.files[jobID+"/"+filename]
	if !ok {
		return nil, "", errNotFound("file")
	}
	return data, "application/fhir+ndjson", nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func errNotFound(msg string) error { return simpleError(msg + " not found") }

func viewOpsHandler(t *testing.T, cfg hahttp.Config) http.Handler {
	t.Helper()
	if cfg.ResourceService == nil {
		cfg.ResourceService = &fakeResourceService{}
	}
	return newTestHandler(t, cfg)
}

func TestViewDefinitionRunTypeRoute(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	rec := doRequest(t, h, http.MethodPost, "/fhir/ViewDefinition/$viewdefinition-run?viewName=patient_summary_view", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.ViewName != "patient_summary_view" {
		t.Fatalf("ViewName=%q", runSvc.last.ViewName)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type=%q", ct)
	}
}

func TestViewDefinitionRunSystemRoute(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	rec := doRequest(t, h, http.MethodPost, "/fhir/$viewdefinition-run?viewName=patient_summary_view", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.ViewName != "patient_summary_view" {
		t.Fatalf("ViewName=%q", runSvc.last.ViewName)
	}
}

func TestViewDefinitionRunNotImplementedWithoutService(t *testing.T) {
	h := viewOpsHandler(t, hahttp.Config{})
	rec := doRequest(t, h, http.MethodPost, "/fhir/$viewdefinition-run?viewName=patient_summary_view", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSQLQueryRunSystemRoute(t *testing.T) {
	sqlSvc := &fakeSQLQueryService{}
	h := viewOpsHandler(t, hahttp.Config{SQLQueryService: sqlSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[{"name":"sql","valueString":{"valueString":"SELECT 1"}}]}`)
	rec := doRequest(t, h, http.MethodPost, "/fhir/$sqlquery-run", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if sqlSvc.last.SQL != "SELECT 1" {
		t.Fatalf("SQL=%q", sqlSvc.last.SQL)
	}
}

func TestSQLQueryRunLibraryRoute(t *testing.T) {
	sqlSvc := &fakeSQLQueryService{}
	h := viewOpsHandler(t, hahttp.Config{SQLQueryService: sqlSvc})
	rec := doRequest(t, h, http.MethodPost, "/fhir/Library/$sqlquery-run?sql=SELECT%201", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if sqlSvc.last.SQL != "SELECT 1" {
		t.Fatalf("SQL=%q", sqlSvc.last.SQL)
	}
}

func TestViewDefinitionExportKickoffStatusAndFileDownload(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/ViewDefinition/$viewdefinition-export?viewName=patient_summary_view",
		nil, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status=%d body=%s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	if !strings.Contains(statusURL, "/ViewDefinition/$viewdefinition-export/status/export-job-1") {
		t.Fatalf("Content-Location=%q", statusURL)
	}

	rec = doRequest(t, h, http.MethodGet, statusURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status poll=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Files []struct {
			Filename string `json:"filename"`
			URL      string `json:"url"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Files) != 1 || payload.Files[0].URL == "" {
		t.Fatalf("files=%+v", payload.Files)
	}

	rec = doRequest(t, h, http.MethodGet, payload.Files[0].URL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("file download status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"p1"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestViewDefinitionExportSystemRoute(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export?viewName=patient_summary_view",
		nil, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
