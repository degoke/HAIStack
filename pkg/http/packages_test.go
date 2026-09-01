package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type fakePackageService struct {
	refreshed bool
}

func (f *fakePackageService) InstallFromRegistry(context.Context, string, string) (*packages.InstallResult, error) {
	return &packages.InstallResult{PackageID: "pkg", Version: "1", Installed: 1}, nil
}
func (f *fakePackageService) InstallFromDirectory(context.Context, string) (*packages.InstallResult, error) {
	return nil, nil
}
func (f *fakePackageService) InstallFromUpload(context.Context, string, string, io.Reader) (*packages.InstallResult, error) {
	return nil, nil
}
func (f *fakePackageService) EnqueueInstall(context.Context, jobs.PackageInstallPayload) (store.JobRecord, error) {
	return store.JobRecord{ID: "job-1"}, nil
}
func (f *fakePackageService) Refresh(context.Context) (*registry.Snapshot, error) {
	f.refreshed = true
	return nil, nil
}

func TestAdminConformanceRefresh(t *testing.T) {
	svc := &fakePackageService{}
	handler := hahttp.NewAdminHandler(svc)
	rec := doRequest(t, handler, http.MethodPost, "/admin/conformance/refresh", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !svc.refreshed {
		t.Fatal("expected refresh to be called")
	}
}

func TestNPMOperationInstall(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{
		ResourceService:  &fakeResourceService{},
		OperationService: hahttp.NPMOperationService{Packages: &fakePackageService{}},
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/$npm?id=hl7.fhir.us.core&version=6.1.0", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var params struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &params); err != nil {
		t.Fatal(err)
	}
	if params.ResourceType != "Parameters" {
		t.Fatalf("type=%q", params.ResourceType)
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

var _ hahttp.PackageInstallService = (*fakePackageService)(nil)
