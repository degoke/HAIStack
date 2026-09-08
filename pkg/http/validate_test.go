package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type recordingValidateService struct {
	lastReq hahttp.ValidateRequest
}

func (r *recordingValidateService) Validate(ctx context.Context, req hahttp.ValidateRequest) (*types.OperationOutcome, error) {
	r.lastReq = req
	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity: "information",
			Code:     "informational",
		}},
	}, nil
}

func TestPatientValidateOperationUsesFHIRValidateService(t *testing.T) {
	validateSvc := &recordingValidateService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		ValidateService: validateSvc,
		SDCService:      fakeSDCService{},
	})
	body := []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`)
	rec := doRequest(t, h, http.MethodPost, "/fhir/Patient/$validate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if validateSvc.lastReq.ResourceType != "Patient" {
		t.Fatalf("resource type = %q, want Patient", validateSvc.lastReq.ResourceType)
	}
	if string(validateSvc.lastReq.Body) != string(body) {
		t.Fatalf("body not forwarded: %q", validateSvc.lastReq.Body)
	}
	var outcome types.OperationOutcome
	if err := json.Unmarshal(rec.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome.ResourceType != "OperationOutcome" {
		t.Fatalf("response type = %q", outcome.ResourceType)
	}
}

func TestQuestionnaireResponseValidateStillUsesSDCService(t *testing.T) {
	validateSvc := &recordingValidateService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		ValidateService: validateSvc,
		SDCService:      fakeSDCService{},
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/QuestionnaireResponse/$validate", []byte(`{"resourceType":"QuestionnaireResponse","status":"in-progress"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if validateSvc.lastReq.ResourceType != "" {
		t.Fatal("expected SDC validate path, FHIR validate service should not be called")
	}
}

func TestInstanceValidateAllowsEmptyBody(t *testing.T) {
	patient := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	validateSvc := &recordingValidateService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
				if resourceType == "Patient" && id == "p1" {
					return patient, nil
				}
				return nil, nil
			},
		},
		ValidateService: validateSvc,
	})
	rec := doRequest(t, h, http.MethodPost, "/fhir/Patient/p1/$validate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if validateSvc.lastReq.ID != "p1" {
		t.Fatalf("id = %q, want p1", validateSvc.lastReq.ID)
	}
	if len(validateSvc.lastReq.Body) != 0 {
		t.Fatalf("expected empty body, got %q", validateSvc.lastReq.Body)
	}
}

func TestCoreValidateServiceDefaultsToFullMode(t *testing.T) {
	var captured validate.ValidateOptions
	engine := &captureValidateEngine{opts: &captured}
	svc := hahttp.CoreValidateService{
		Engine: engine,
	}
	_, err := svc.Validate(context.Background(), hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Mode != validate.ValidationModeFull {
		t.Fatalf("mode = %v, want full", captured.Mode)
	}
	if !captured.ProfileConstraints {
		t.Fatal("expected ProfileConstraints true by default")
	}
}

type captureValidateEngine struct {
	opts   *validate.ValidateOptions
	called *bool
}

func (c *captureValidateEngine) Validate(ctx context.Context, res *types.ResourceEnvelope, opts validate.ValidateOptions) (*validate.ValidationResult, error) {
	if c.called != nil {
		*c.called = true
	}
	if c.opts != nil {
		*c.opts = opts
	}
	return &validate.ValidationResult{Valid: true}, nil
}

func TestCoreValidateServiceFastModeQuery(t *testing.T) {
	var captured validate.ValidateOptions
	engine := &captureValidateEngine{opts: &captured}
	svc := hahttp.CoreValidateService{Engine: engine}
	req := hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	req.Query = map[string][]string{"_fast": {"true"}}
	if _, err := svc.Validate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if captured.Mode != validate.ValidationModeFast {
		t.Fatalf("mode = %v, want fast", captured.Mode)
	}
	if captured.ProfileConstraints {
		t.Fatal("expected ProfileConstraints false in fast mode")
	}
}

func TestCoreValidateServiceInvariantsOverrideFastMode(t *testing.T) {
	var captured validate.ValidateOptions
	engine := &captureValidateEngine{opts: &captured}
	svc := hahttp.CoreValidateService{Engine: engine}
	req := hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
	}
	req.Query = map[string][]string{
		"_fast":       {"true"},
		"_invariants": {"true"},
	}
	if _, err := svc.Validate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if captured.Mode != validate.ValidationModeFast {
		t.Fatalf("mode = %v, want fast", captured.Mode)
	}
	if !captured.ProfileConstraints {
		t.Fatal("expected ProfileConstraints true when _invariants=true")
	}
}

func TestCoreValidateServiceParametersDeleteModeOnInstance(t *testing.T) {
	patient := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
	}
	svc := hahttp.CoreValidateService{
		Engine: &captureValidateEngine{},
		Resources: &fakeResourceService{
			readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
				if resourceType == "Patient" && id == "p1" {
					return patient, nil
				}
				return nil, nil
			},
		},
	}
	body := []byte(`{"resourceType":"Parameters","parameter":[{"name":"mode","valueCode":"delete"}]}`)
	outcome, err := svc.Validate(context.Background(), hahttp.ValidateRequest{
		ResourceType: "Patient",
		ID:           "p1",
		Body:         body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome == nil || outcome.ResourceType != "OperationOutcome" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestCoreValidateServiceParametersDeleteModeRequiresInstance(t *testing.T) {
	svc := hahttp.CoreValidateService{
		Engine:    &captureValidateEngine{},
		Resources: &fakeResourceService{},
	}
	body := []byte(`{"resourceType":"Parameters","parameter":[{"name":"mode","valueCode":"delete"}]}`)
	_, err := svc.Validate(context.Background(), hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         body,
	})
	if err == nil {
		t.Fatal("expected error for type-level delete validate")
	}
}

func TestCoreValidateServiceParametersCreateModeValidatesResource(t *testing.T) {
	var called bool
	engine := &captureValidateEngine{called: &called}
	svc := hahttp.CoreValidateService{Engine: engine}
	body := []byte(`{
		"resourceType":"Parameters",
		"parameter":[
			{"name":"mode","valueCode":"create"},
			{"name":"resource","resource":{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}}
		]
	}`)
	if _, err := svc.Validate(context.Background(), hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         body,
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected engine validate for create mode")
	}
}

func TestValidateOperationReturnsXMLOperationOutcome(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		ValidateService: hahttp.CoreValidateService{
			Engine: &captureValidateEngine{},
		},
	})
	body := []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`)
	rec := doRequestWithHeaders(t, h, http.MethodPost, "/fhir/Patient/$validate?_format=xml", body, map[string]string{
		"Accept": "application/fhir+xml",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Fatalf("content-type=%q want xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "OperationOutcome") {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestCoreValidateServiceRejectsUnknownElement(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Patient.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := validate.NewEngine(validate.Config{ProfileCatalog: catalog, FHIRPath: fp})
	if err != nil {
		t.Fatal(err)
	}
	svc := hahttp.CoreValidateService{
		Engine: engine,
		Options: validate.ValidateOptions{
			EnforceBaseProfile: true,
		},
	}
	outcome, err := svc.Validate(context.Background(), hahttp.ValidateRequest{
		ResourceType: "Patient",
		Body:         []byte(`{"resourceType":"Patient","id":"p1","bogus":"nope","name":[{"family":"Doe"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome == nil || len(outcome.Issue) == 0 {
		t.Fatal("expected validation issues")
	}
	found := false
	for _, issue := range outcome.Issue {
		if issue.Code == "unknown-element" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown-element issue, got %+v", outcome.Issue)
	}
}

func TestCapabilityStatementAdvertisesValidateOperation(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{
		ResourceService:  &fakeResourceService{},
		CapabilitySource: fakeCapabilitySource{snapshot: registryCapabilitySnapshot()},
	})
	rec := doRequest(t, h, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var cs struct {
		Rest []struct {
			Resource []struct {
				Type      string `json:"type"`
				Operation []struct {
					Name       string `json:"name"`
					Definition string `json:"definition"`
				} `json:"operation"`
			} `json:"resource"`
		} `json:"rest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cs); err != nil {
		t.Fatal(err)
	}
	if len(cs.Rest) == 0 || len(cs.Rest[0].Resource) == 0 {
		t.Fatal("missing rest.resource")
	}
	found := false
	for _, op := range cs.Rest[0].Resource[0].Operation {
		if op.Name == "validate" {
			found = true
			if op.Definition != "http://hl7.org/fhir/OperationDefinition/Resource-validate" {
				t.Fatalf("definition = %q", op.Definition)
			}
		}
	}
	if !found {
		t.Fatal("CapabilityStatement missing validate operation")
	}
}

func registryCapabilitySnapshot() registry.CapabilitySnapshot {
	return registry.CapabilitySnapshot{
		FHIRVersion: "4.0.1",
		Resources: []registry.ResourceCapability{{
			ResourceType: "Patient",
		}},
	}
}
