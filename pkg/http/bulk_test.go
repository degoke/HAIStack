package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/export"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type bulkAuthChecker struct {
	allow bool
	calls int
}

func (c *bulkAuthChecker) AuthorizeRead(context.Context, auth.Principal, auth.TenantContext, string, string) (auth.Decision, error) {
	return auth.Allow("ok"), nil
}
func (c *bulkAuthChecker) AuthorizeWrite(context.Context, auth.Principal, auth.TenantContext, string, string, string) (auth.Decision, error) {
	return auth.Allow("ok"), nil
}
func (c *bulkAuthChecker) AuthorizeSearch(context.Context, auth.Principal, auth.TenantContext, string) (auth.Decision, error) {
	return auth.Allow("ok"), nil
}
func (c *bulkAuthChecker) AuthorizeExport(_ context.Context, _ auth.Principal, _ auth.TenantContext, _ string) (auth.Decision, error) {
	c.calls++
	if c.allow {
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
	checker := &bulkAuthChecker{allow: true}
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
	if checker.calls == 0 {
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
		AuthChecker: &bulkAuthChecker{allow: false},
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
