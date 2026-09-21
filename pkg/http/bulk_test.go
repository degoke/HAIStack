package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/bulkimport"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/export"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type bulkAuthChecker struct {
	allowExport    bool
	allowWrite     bool
	allowRead      bool
	allowWriteType map[string]bool
	exportCalls    int
	writeCalls     int
	readCalls      int
	writeTypes     []string
}

func (c *bulkAuthChecker) AuthorizeRead(context.Context, auth.Principal, auth.TenantContext, string, string) (auth.Decision, error) {
	c.readCalls++
	if c.allowRead {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}
func (c *bulkAuthChecker) AuthorizeWrite(_ context.Context, _ auth.Principal, _ auth.TenantContext, _, resourceType, _ string) (auth.Decision, error) {
	c.writeCalls++
	c.writeTypes = append(c.writeTypes, resourceType)
	if len(c.allowWriteType) > 0 {
		if c.allowWriteType[resourceType] {
			return auth.Allow("ok"), nil
		}
		return auth.Deny("denied"), nil
	}
	if c.allowWrite {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}
func (c *bulkAuthChecker) AuthorizeSearch(context.Context, auth.Principal, auth.TenantContext, string) (auth.Decision, error) {
	return auth.Allow("ok"), nil
}
func (c *bulkAuthChecker) AuthorizeExport(_ context.Context, _ auth.Principal, _ auth.TenantContext, _ string) (auth.Decision, error) {
	c.exportCalls++
	if c.allowExport {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}

type bulkResourceService struct {
	fakeResourceService
}

func (bulkResourceService) ListIDs(_ context.Context, resourceType string, _ int, offset int) ([]string, error) {
	if resourceType != "Patient" || offset > 0 {
		return nil, nil
	}
	return []string{"p1"}, nil
}

func (bulkResourceService) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if resourceType == "Patient" && id == "p1" {
		return &types.ResourceEnvelope{
			ResourceType: "Patient",
			ID:           "p1",
			LastUpdated:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
		}, nil
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
}

func newBulkExportService(t *testing.T) hahttp.BulkExportService {
	t.Helper()
	files := export.NewInMemoryFileStore()
	jobs := export.NewInMemoryJobStore()
	executor := &export.Executor{Resources: &bulkResourceService{}, Files: files}
	svc, err := export.NewService(export.Config{
		Jobs:     jobs,
		Files:    files,
		Executor: executor,
		BasePath: "/fhir",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestBulkExportKickoffPollManifest(t *testing.T) {
	checker := &bulkAuthChecker{allowExport: true}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkExportService: newBulkExportService(t),
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: checker,
	})

	req := httptest.NewRequest(http.MethodGet, "/fhir/$export?_type=Patient", nil)
	req.Header.Set("Prefer", "respond-async")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status = %d body = %s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	if statusURL == "" {
		t.Fatal("missing Content-Location")
	}
	if checker.exportCalls == 0 {
		t.Fatal("expected export authorization")
	}

	pollReq := httptest.NewRequest(http.MethodGet, statusURL, nil)
	pollReq.Header.Set("Accept", "application/json")
	pollRec := httptest.NewRecorder()
	handler.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll status = %d body = %s", pollRec.Code, pollRec.Body.String())
	}
	var manifest map[string]any
	if err := json.Unmarshal(pollRec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	output, ok := manifest["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("manifest output = %#v", manifest["output"])
	}
}

func TestBulkExportUnauthorized(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkExportService: newBulkExportService(t),
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "user-1", Kind: auth.KindUser}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{allowExport: false},
	})
	req := httptest.NewRequest(http.MethodGet, "/fhir/$export", nil)
	req.Header.Set("Prefer", "respond-async")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBulkExportReturnsNotImplementedWithoutService(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/$export", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

type importWriter struct {
	resources map[string]*types.ResourceEnvelope
}

func (w *importWriter) key(resourceType, id string) string { return resourceType + "/" + id }

func (w *importWriter) Create(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if w.resources == nil {
		w.resources = map[string]*types.ResourceEnvelope{}
	}
	cp := *resource
	w.resources[w.key(resource.ResourceType, resource.ID)] = &cp
	return &cp, nil
}

func (w *importWriter) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return w.Create(ctx, resource)
}

func (w *importWriter) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env := w.resources[w.key(resourceType, id)]; env != nil {
		return env, nil
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
}

func newBulkImportService(t *testing.T, writer *importWriter) hahttp.BulkImportService {
	t.Helper()
	files := bulkimport.NewInMemoryFileStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: writer, Files: files},
		BasePath: "/fhir",
		NewID:    func() string { return "import-job-1" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestBulkImportKickoffPollManifest(t *testing.T) {
	checker := &bulkAuthChecker{allowWrite: true, allowRead: true}
	writer := &importWriter{}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, writer),
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: checker,
	})

	body := `{"resourceType":"Parameters","parameter":[{"name":"inputFormat","valueCode":"application/fhir+ndjson"},{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status = %d body = %s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	if statusURL == "" {
		t.Fatal("missing Content-Location")
	}
	if checker.writeCalls == 0 {
		t.Fatal("expected import write authorization")
	}
	if checker.exportCalls != 0 {
		t.Fatalf("import kickoff used export auth (%d calls)", checker.exportCalls)
	}

	pollReq := httptest.NewRequest(http.MethodGet, statusURL, nil)
	pollReq.Header.Set("Accept", "application/json")
	pollRec := httptest.NewRecorder()
	handler.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll status = %d body = %s", pollRec.Code, pollRec.Body.String())
	}
	var manifest map[string]any
	if err := json.Unmarshal(pollRec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	output, ok := manifest["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("manifest output = %#v", manifest["output"])
	}
	if _, err := writer.Read(context.Background(), "Patient", "p1"); err != nil {
		t.Fatalf("imported patient: %v", err)
	}
}

func TestBulkImportRequiresPreferAsync(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, &importWriter{}),
	})
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(`{"resourceType":"Parameters","parameter":[{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{}"}]}]}`))
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportReturnsNotImplementedWithoutService(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(`{}`))
	req.Header.Set("Prefer", "respond-async")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCapabilityStatementAdvertisesImportWhenConfigured(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &fakeResourceService{},
		BulkImportService: newBulkImportService(t, &importWriter{}),
		CapabilitySource: fakeCapabilitySource{snapshot: registry.CapabilitySnapshot{
			FHIRVersion: "4.0.1",
			Resources:   []registry.ResourceCapability{{ResourceType: "Patient"}},
		}},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"import"`) {
		t.Fatalf("expected $import advertised, got %s", rec.Body.String())
	}
}

func TestBulkImportDeniedWhenAnyInputTypeUnauthorized(t *testing.T) {
	writer := &importWriter{}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, writer),
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{
			allowWrite:     true,
			allowRead:      true,
			allowWriteType: map[string]bool{"Patient": true},
		},
	})
	body := `{"resourceType":"Parameters","parameter":[` +
		`{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}]},` +
		`{"name":"input","part":[{"name":"type","valueCode":"Observation"},{"name":"valueString","valueString":"{\"resourceType\":\"Observation\",\"id\":\"o1\",\"status\":\"final\",\"code\":{\"text\":\"x\"}}"}]}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, err := writer.Read(context.Background(), "Patient", "p1"); err == nil {
		t.Fatal("kickoff must not import Patient when Observation write is denied")
	}
}

func TestBulkImportUnauthorizedWithoutWrite(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, &importWriter{}),
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{allowExport: true, allowWrite: false, allowRead: true},
	})
	body := `{"resourceType":"Parameters","parameter":[{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportInvalidParameters(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, &importWriter{}),
	})
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(`{"resourceType":"Patient","id":"p1"}`))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportURLWithoutLoaderIsBadRequest(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, &importWriter{}),
	})
	body := `{"resourceType":"Parameters","parameter":[{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"url","valueUri":"https://example.test/Patient.ndjson"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportAcceptsPreferCommaForm(t *testing.T) {
	writer := &importWriter{}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, writer),
	})
	body := `{"resourceType":"Parameters","parameter":[{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async, wait=10")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkExportAcceptsPreferCommaForm(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkExportService: newBulkExportService(t),
	})
	req := httptest.NewRequest(http.MethodGet, "/fhir/$export?_type=Patient", nil)
	req.Header.Set("Prefer", "respond-async, wait=10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportErrorFileDownload(t *testing.T) {
	writer := &importWriter{}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: newBulkImportService(t, writer),
	})
	body := `{"resourceType":"Parameters","parameter":[{"name":"input","part":[{"name":"type","valueCode":"Patient"},{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}\nnot-json"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/fhir/$import", strings.NewReader(body))
	req.Header.Set("Prefer", "respond-async")
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("kickoff status = %d body = %s", rec.Code, rec.Body.String())
	}
	statusURL := rec.Header().Get("Content-Location")
	pollReq := httptest.NewRequest(http.MethodGet, statusURL, nil)
	pollReq.Header.Set("Accept", "application/json")
	pollRec := httptest.NewRecorder()
	handler.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll status = %d body = %s", pollRec.Code, pollRec.Body.String())
	}
	var manifest map[string]any
	if err := json.Unmarshal(pollRec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	errors, ok := manifest["error"].([]any)
	if !ok || len(errors) != 1 {
		t.Fatalf("manifest error = %#v", manifest["error"])
	}
	errObj, _ := errors[0].(map[string]any)
	fileURL, _ := errObj["url"].(string)
	if fileURL == "" {
		t.Fatal("missing error file url")
	}
	fileReq := httptest.NewRequest(http.MethodGet, fileURL, nil)
	fileRec := httptest.NewRecorder()
	handler.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("file status = %d body = %s", fileRec.Code, fileRec.Body.String())
	}
	if !strings.Contains(fileRec.Body.String(), "OperationOutcome") {
		t.Fatalf("error artifact = %s", fileRec.Body.String())
	}
}

type stubExportFiles struct {
	err error
}

func (s stubExportFiles) Kickoff(context.Context, export.KickoffRequest) (*export.Job, error) {
	return nil, fmt.Errorf("unused")
}
func (s stubExportFiles) GetJob(context.Context, string) (*export.Job, error) { return nil, nil }
func (s stubExportFiles) Cancel(context.Context, string) error                { return nil }
func (s stubExportFiles) Manifest(*export.Job) *export.Manifest               { return nil }
func (s stubExportFiles) StatusURL(string) string                             { return "" }
func (s stubExportFiles) GetFile(context.Context, string, string) ([]byte, string, error) {
	return nil, "", s.err
}

type stubImportFiles struct {
	err error
}

func (s stubImportFiles) Kickoff(context.Context, bulkimport.KickoffRequest) (*bulkimport.Job, error) {
	return nil, fmt.Errorf("unused")
}
func (s stubImportFiles) GetJob(context.Context, string) (*bulkimport.Job, error) { return nil, nil }
func (s stubImportFiles) Cancel(context.Context, string) error                    { return nil }
func (s stubImportFiles) Manifest(*bulkimport.Job) *bulkimport.Manifest           { return nil }
func (s stubImportFiles) StatusURL(string) string                                 { return "" }
func (s stubImportFiles) GetFile(context.Context, string, string) ([]byte, string, error) {
	return nil, "", s.err
}

func TestBulkExportFilePreservesStoreErrors(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkExportService: stubExportFiles{err: fmt.Errorf("connection refused")},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{allowExport: true},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/$export/files/job-1/Patient.ndjson", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkExportFileMissingIsNotFound(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &bulkResourceService{},
		BulkExportService: stubExportFiles{
			err: fmt.Errorf("export: file %q not found: %w", "Patient.ndjson", binary.ErrNotFound),
		},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{allowExport: true},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/$export/files/job-1/Patient.ndjson", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestBulkImportFilePreservesStoreErrors(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkImportService: stubImportFiles{err: fmt.Errorf("connection refused")},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "svc-1", Kind: auth.KindService}, auth.TenantContext{TenantID: "tenant-a"}, nil
		},
		AuthChecker: &bulkAuthChecker{allowRead: true},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/$import/files/job-1/error-0-Patient.ndjson", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
