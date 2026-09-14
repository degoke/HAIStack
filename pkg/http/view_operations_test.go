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
	last  view.ViewExportRequest
}

func newFakeViewExportService() *fakeViewExportService {
	return &fakeViewExportService{
		jobs:  make(map[string]*view.ViewExportJob),
		files: make(map[string][]byte),
	}
}

func (f *fakeViewExportService) Kickoff(_ context.Context, req view.ViewExportRequest) (*view.ViewExportJob, error) {
	f.last = req
	job := &view.ViewExportJob{
		ID:      "export-job-1",
		Status:  view.ExportComplete,
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

func TestViewDefinitionRunParsesSubjectAndParameters(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"viewName","valueString":{"valueString":"patient_summary_view"}},
		{"name":"subject","valueString":{"valueString":"Patient/pat-jane"}},
		{"name":"actor","valueString":{"valueString":"Practitioner/runner"}},
		{"name":"tenant","valueString":{"valueString":"demo"}}
	]}`)
	rec := doRequest(t, h, http.MethodPost, "/fhir/$viewdefinition-run", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.Subject != "Patient/pat-jane" {
		t.Fatalf("Subject=%q, want Patient/pat-jane", runSvc.last.Subject)
	}
	if runSvc.last.Actor != "Practitioner/runner" {
		t.Fatalf("Actor=%q, want Practitioner/runner", runSvc.last.Actor)
	}
	if runSvc.last.Parameters["tenant"] != "demo" {
		t.Fatalf("Parameters=%v, want tenant=demo", runSvc.last.Parameters)
	}
}

func TestViewDefinitionRunParsesQuerySubject(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	rec := doRequest(t, h, http.MethodPost,
		"/fhir/$viewdefinition-run?viewName=patient_summary_view&_subject=Patient%2Fquery-subject&_actor=Practitioner%2Fquery",
		nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.Subject != "Patient/query-subject" {
		t.Fatalf("Subject=%q, want Patient/query-subject", runSvc.last.Subject)
	}
	if runSvc.last.Actor != "Practitioner/query" {
		t.Fatalf("Actor=%q, want Practitioner/query", runSvc.last.Actor)
	}
}

func TestViewDefinitionRunBodyOverridesQuerySubject(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"viewName","valueString":{"valueString":"patient_summary_view"}},
		{"name":"subject","valueString":{"valueString":"Patient/body-subject"}},
		{"name":"actor","valueString":{"valueString":"Practitioner/body"}}
	]}`)
	rec := doRequest(t, h, http.MethodPost,
		"/fhir/$viewdefinition-run?_subject=Patient%2Fquery-subject&_actor=Practitioner%2Fquery",
		body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.Subject != "Patient/body-subject" {
		t.Fatalf("Subject=%q, want body to override query", runSvc.last.Subject)
	}
	if runSvc.last.Actor != "Practitioner/body" {
		t.Fatalf("Actor=%q, want body to override query", runSvc.last.Actor)
	}
}

func TestViewDefinitionRunEmptyBodySubjectPreservesQuery(t *testing.T) {
	runSvc := &fakeViewRunService{}
	h := viewOpsHandler(t, hahttp.Config{ViewRunService: runSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"viewName","valueString":{"valueString":"patient_summary_view"}},
		{"name":"subject","valueString":{"valueString":""}},
		{"name":"actor","valueString":{"valueString":""}}
	]}`)
	rec := doRequest(t, h, http.MethodPost,
		"/fhir/$viewdefinition-run?_subject=Patient%2Fquery-subject&_actor=Practitioner%2Fquery",
		body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runSvc.last.Subject != "Patient/query-subject" {
		t.Fatalf("Subject=%q, want query preserved when body subject is empty", runSvc.last.Subject)
	}
	if runSvc.last.Actor != "Practitioner/query" {
		t.Fatalf("Actor=%q, want query preserved when body actor is empty", runSvc.last.Actor)
	}
}

func TestViewDefinitionExportParsesActorFromBody(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}}
		]},
		{"name":"actor","valueString":{"valueString":"Practitioner/export"}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Actor != "Practitioner/export" {
		t.Fatalf("Actor=%q, want Practitioner/export", exportSvc.last.Actor)
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

func TestViewDefinitionExportParsesSubjectAndParameters(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}},
			{"name":"version","valueString":{"valueString":"1.0.0"}}
		]},
		{"name":"subject","valueString":{"valueString":"Patient/pat-jane"}},
		{"name":"tenant","valueString":{"valueString":"demo"}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Subject != "Patient/pat-jane" {
		t.Fatalf("Subject=%q, want Patient/pat-jane", exportSvc.last.Subject)
	}
	if exportSvc.last.Parameters["tenant"] != "demo" {
		t.Fatalf("Parameters=%v, want tenant=demo", exportSvc.last.Parameters)
	}
}

func TestViewDefinitionExportParsesFormatFromBody(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}}
		]},
		{"name":"format","valueString":{"valueString":"parquet"}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Format != view.FormatParquet {
		t.Fatalf("Format=%q, want parquet", exportSvc.last.Format)
	}
}

func TestViewDefinitionExportParsesQuerySubject(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export?viewName=patient_summary_view&_subject=Patient%2Fquery-subject",
		nil, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Subject != "Patient/query-subject" {
		t.Fatalf("Subject=%q, want Patient/query-subject", exportSvc.last.Subject)
	}
}

func TestViewDefinitionExportBodyOverridesQuerySubject(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}}
		]},
		{"name":"subject","valueString":{"valueString":"Patient/body-subject"}},
		{"name":"actor","valueString":{"valueString":"Practitioner/body"}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export?_subject=Patient%2Fquery-subject&_actor=Practitioner%2Fquery",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Subject != "Patient/body-subject" {
		t.Fatalf("Subject=%q, want body to override query", exportSvc.last.Subject)
	}
	if exportSvc.last.Actor != "Practitioner/body" {
		t.Fatalf("Actor=%q, want body to override query", exportSvc.last.Actor)
	}
}

func TestViewDefinitionExportEmptyBodySubjectPreservesQuery(t *testing.T) {
	exportSvc := newFakeViewExportService()
	h := viewOpsHandler(t, hahttp.Config{ViewExportService: exportSvc})
	body := []byte(`{"resourceType":"Parameters","parameter":[
		{"name":"view","part":[
			{"name":"viewName","valueString":{"valueString":"patient_summary_view"}}
		]},
		{"name":"subject","valueString":{"valueString":""}},
		{"name":"actor","valueString":{"valueString":""}}
	]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost,
		"/fhir/$viewdefinition-export?_subject=Patient%2Fquery-subject&_actor=Practitioner%2Fquery",
		body, map[string]string{"Prefer": "respond-async"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if exportSvc.last.Subject != "Patient/query-subject" {
		t.Fatalf("Subject=%q, want query preserved when body subject is empty", exportSvc.last.Subject)
	}
	if exportSvc.last.Actor != "Practitioner/query" {
		t.Fatalf("Actor=%q, want query preserved when body actor is empty", exportSvc.last.Actor)
	}
}
