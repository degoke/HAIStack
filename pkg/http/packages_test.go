package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type fakePackageService struct {
	enqueued bool
}

func (f *fakePackageService) EnqueueRegistryInstall(context.Context, string, string) (store.JobRecord, error) {
	f.enqueued = true
	return store.JobRecord{ID: "job-1", Status: store.JobStatusPending}, nil
}

func (f *fakePackageService) EnqueueArchiveInstall(context.Context, string, string, io.Reader) (store.JobRecord, error) {
	f.enqueued = true
	return store.JobRecord{ID: "job-2", Status: store.JobStatusPending}, nil
}

type syncPackageService struct {
	called bool
}

func (s *syncPackageService) EnqueueRegistryInstall(context.Context, string, string) (store.JobRecord, error) {
	s.called = true
	return store.JobRecord{ID: "install-hl7.fhir.us.core-6.1.0", Status: store.JobStatusCompleted}, nil
}

func (s *syncPackageService) EnqueueArchiveInstall(context.Context, string, string, io.Reader) (store.JobRecord, error) {
	s.called = true
	return store.JobRecord{ID: "install-upload-pkg", Status: store.JobStatusCompleted}, nil
}

func TestDirectPackageInstallServiceNotConfigured(t *testing.T) {
	svc := hahttp.DirectPackageInstallService{}
	_, err := svc.EnqueueRegistryInstall(context.Background(), "hl7.fhir.us.core", "6.1.0")
	if err == nil {
		t.Fatal("expected error when installer is not configured")
	}
}

func TestImplementationGuideInstallEnqueuesJob(t *testing.T) {
	svc := &fakePackageService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService:       &fakeResourceService{},
		PackageInstallService: svc,
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/ImplementationGuide/$install?packageId=hl7.fhir.us.core&version=6.1.0", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !svc.enqueued {
		t.Fatal("expected async enqueue")
	}
	assertParametersStatus(t, rec.Body.Bytes(), "accepted")
}

func TestImplementationGuideInstallReturnsCompletedForSyncInstall(t *testing.T) {
	svc := &syncPackageService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService:       &fakeResourceService{},
		PackageInstallService: svc,
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/ImplementationGuide/$install?packageId=hl7.fhir.us.core&version=6.1.0", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !svc.called {
		t.Fatal("expected sync install")
	}
	assertParametersStatus(t, rec.Body.Bytes(), "completed")
}

func TestImplementationGuidePackageExportNotImplemented(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{
		ResourceService:       &fakeResourceService{},
		PackageInstallService: &fakePackageService{},
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/ImplementationGuide/$package", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCoreValidateServiceFastModeDisablesInvariants(t *testing.T) {
	var captured validate.ValidateOptions
	engine := &captureValidateEngine{opts: &captured}
	svc := hahttp.CoreValidateService{
		Engine: engine,
		Options: validate.ValidateOptions{
			ProfileConstraints: true,
		},
	}
	req := hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	req.Query = map[string][]string{"_fast": {"true"}}
	if _, err := svc.Validate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if captured.ProfileConstraints {
		t.Fatal("expected ProfileConstraints false in fast mode")
	}
}

func assertParametersStatus(t *testing.T, body []byte, want string) {
	t.Helper()
	var params struct {
		ResourceType string `json:"resourceType"`
		Parameter    []struct {
			Name        string `json:"name"`
			ValueString string `json:"valueString"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		t.Fatal(err)
	}
	if params.ResourceType != "Parameters" {
		t.Fatalf("type=%q", params.ResourceType)
	}
	for _, p := range params.Parameter {
		if p.Name == "status" && p.ValueString == want {
			return
		}
	}
	t.Fatalf("status parameter %q not found in %s", want, string(body))
}

var _ hahttp.PackageInstallService = (*fakePackageService)(nil)
var _ hahttp.PackageInstallService = (*syncPackageService)(nil)
