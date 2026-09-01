package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type fakePackageService struct {
	installedFromRegistry bool
	installedFromArchive  bool
	enqueued              bool
}

func (f *fakePackageService) InstallFromRegistry(context.Context, string, string) (*packages.InstallResult, error) {
	f.installedFromRegistry = true
	return &packages.InstallResult{PackageID: "hl7.fhir.us.core", Version: "6.1.0", Installed: 1}, nil
}

func (f *fakePackageService) InstallFromArchive(context.Context, string, string, io.Reader) (*packages.InstallResult, error) {
	f.installedFromArchive = true
	return &packages.InstallResult{PackageID: "local.pkg", Version: "1.0.0", Installed: 2}, nil
}

func (f *fakePackageService) EnqueueRegistryInstall(context.Context, string, string) (store.JobRecord, error) {
	f.enqueued = true
	return store.JobRecord{ID: "job-1"}, nil
}

func (f *fakePackageService) EnqueueArchiveInstall(context.Context, string, string, io.Reader) (store.JobRecord, error) {
	f.enqueued = true
	return store.JobRecord{ID: "job-2"}, nil
}

func TestImplementationGuideInstallAsyncDefault(t *testing.T) {
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
	if svc.installedFromRegistry {
		t.Fatal("did not expect sync install")
	}
	assertParametersStatus(t, rec.Body.Bytes(), "accepted")
}

func TestImplementationGuideInstallSync(t *testing.T) {
	svc := &fakePackageService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService:       &fakeResourceService{},
		PackageInstallService: svc,
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/ImplementationGuide/$install?packageId=hl7.fhir.us.core&version=6.1.0&_sync=true", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !svc.installedFromRegistry {
		t.Fatal("expected sync install")
	}
	if svc.enqueued {
		t.Fatal("did not expect async enqueue")
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
