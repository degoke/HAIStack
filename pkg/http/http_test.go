package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type fakeResourceService struct {
	createFn    func(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	readFn      func(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
	updateFn    func(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	deleteFn    func(ctx context.Context, resourceType, id string) error
	historyFn   func(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error)
	transaction func(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
}

func (f *fakeResourceService) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if f.createFn != nil {
		return f.createFn(ctx, resource)
	}
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeResourceService) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if f.readFn != nil {
		return f.readFn(ctx, resourceType, id)
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
}

func (f *fakeResourceService) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, resource)
	}
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeResourceService) Delete(ctx context.Context, resourceType, id string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, resourceType, id)
	}
	return fmt.Errorf("not implemented")
}

func (f *fakeResourceService) History(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	if f.historyFn != nil {
		return f.historyFn(ctx, resourceType, id)
	}
	return nil, nil
}

func (f *fakeResourceService) ProcessTransactionBundle(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if f.transaction != nil {
		return f.transaction(ctx, bundle)
	}
	return nil, fmt.Errorf("not implemented")
}

type fakeSearchService struct {
	searchFn func(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error)
}

func (f *fakeSearchService) SearchBundle(ctx context.Context, resourceType string, params url.Values) (*search.SearchBundle, error) {
	if f.searchFn != nil {
		return f.searchFn(ctx, resourceType, params)
	}
	return &search.SearchBundle{ResourceType: resourceType}, nil
}

type fakeCapabilitySource struct {
	snapshot registry.CapabilitySnapshot
}

func (f fakeCapabilitySource) CapabilitySnapshot() registry.CapabilitySnapshot {
	return f.snapshot
}

type recordingAuthChecker struct {
	readCalls   []string
	writeCalls  []string
	searchCalls []string
	allow       bool
}

func (c *recordingAuthChecker) AuthorizeRead(_ context.Context, _ auth.Principal, _ auth.TenantContext, resourceType, id string) (auth.Decision, error) {
	c.readCalls = append(c.readCalls, resourceType+"/"+id)
	if c.allow {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}

func (c *recordingAuthChecker) AuthorizeWrite(_ context.Context, _ auth.Principal, _ auth.TenantContext, operation, resourceType, id string) (auth.Decision, error) {
	c.writeCalls = append(c.writeCalls, operation+":"+resourceType+"/"+id)
	if c.allow {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}

func (c *recordingAuthChecker) AuthorizeSearch(_ context.Context, _ auth.Principal, _ auth.TenantContext, resourceType string) (auth.Decision, error) {
	c.searchCalls = append(c.searchCalls, resourceType)
	if c.allow {
		return auth.Allow("ok"), nil
	}
	return auth.Deny("denied"), nil
}

func newTestHandler(t *testing.T, cfg hahttp.Config) http.Handler {
	t.Helper()
	h, err := hahttp.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

func doRequest(t *testing.T, handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/fhir+json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func patientJSON(id, family string) []byte {
	payload := map[string]interface{}{
		"resourceType": "Patient",
		"name":         []map[string]string{{"family": family}},
	}
	if id != "" {
		payload["id"] = id
	}
	data, _ := json.Marshal(payload)
	return data
}

func patientEnvelope(id, family string) *types.ResourceEnvelope {
	data := patientJSON(id, family)
	return &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           id,
		VersionID:    "1",
		LastUpdated:  time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		JSON:         data,
	}
}

func decodeOutcome(t *testing.T, body []byte) types.OperationOutcome {
	t.Helper()
	var outcome types.OperationOutcome
	if err := json.Unmarshal(body, &outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	return outcome
}

func TestReadHappyPath(t *testing.T) {
	svc := &fakeResourceService{
		readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
			return patientEnvelope(id, "Doe"), nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: svc,
	})

	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/fhir+json" {
		t.Fatalf("content-type = %q", ct)
	}
	if etag := rec.Header().Get("ETag"); etag != `W/"1"` {
		t.Fatalf("etag = %q", etag)
	}
}

func TestReadNotFound(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if len(outcome.Issue) == 0 || outcome.Issue[0].Code != "not-found" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestCreateWithServerAssignedID(t *testing.T) {
	svc := &fakeResourceService{
		createFn: func(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
			created := patientEnvelope("generated-id", "Doe")
			created.VersionID = "v1"
			return created, nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})
	rec := doRequest(t, handler, http.MethodPost, "/fhir/Patient", patientJSON("", "Doe"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/fhir/Patient/generated-id" {
		t.Fatalf("location = %q", loc)
	}
}

func TestCreateInvalidBody(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodPost, "/fhir/Patient", []byte(`not-json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if outcome.Issue[0].Code != "invalid" {
		t.Fatalf("code = %q", outcome.Issue[0].Code)
	}
}

func TestUpdateIDMismatch(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodPut, "/fhir/Patient/pat-1", patientJSON("other-id", "Doe"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if !strings.Contains(outcome.Issue[0].Diagnostics, "mismatch") {
		t.Fatalf("diagnostics = %q", outcome.Issue[0].Diagnostics)
	}
}

func TestUpdateMissingResource(t *testing.T) {
	svc := &fakeResourceService{
		updateFn: func(_ context.Context, _ *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
			return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "resource not found"}
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})
	rec := doRequest(t, handler, http.MethodPut, "/fhir/Patient/missing", patientJSON("missing", "Doe"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDeleteHappyAndMissing(t *testing.T) {
	deleted := false
	svc := &fakeResourceService{
		deleteFn: func(_ context.Context, resourceType, id string) error {
			if id == "pat-1" {
				deleted = true
				return nil
			}
			return &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})

	rec := doRequest(t, handler, http.MethodDelete, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if !deleted {
		t.Fatal("expected delete to be called")
	}

	rec = doRequest(t, handler, http.MethodDelete, "/fhir/Patient/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHistoryBundle(t *testing.T) {
	svc := &fakeResourceService{
		historyFn: func(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
			return []store.ResourceVersion{{
				ResourceType: resourceType,
				ID:           id,
				VersionID:    "1",
				Action:       store.VersionActionCreate,
				Timestamp:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Resource:     patientEnvelope(id, "Doe"),
			}}, nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/_history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bundle["type"] != "history" {
		t.Fatalf("type = %v", bundle["type"])
	}
}

func TestSearchDelegatesQueryParams(t *testing.T) {
	var captured url.Values
	searchSvc := &fakeSearchService{
		searchFn: func(_ context.Context, resourceType string, params url.Values) (*search.SearchBundle, error) {
			captured = params
			total := 1
			return &search.SearchBundle{
				ResourceType: resourceType,
				Total:        &total,
				Entries: []search.BundleEntry{{
					FullURL:  "Patient/pat-1",
					Resource: patientEnvelope("pat-1", "Doe"),
					Mode:     "match",
				}},
			}, nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		SearchService:   searchSvc,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient?family=Doe&_count=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if captured.Get("family") != "Doe" {
		t.Fatalf("family = %q", captured.Get("family"))
	}
	if captured.Get("_count") != "10" {
		t.Fatalf("_count = %q", captured.Get("_count"))
	}
	var bundle map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bundle["type"] != "searchset" {
		t.Fatalf("type = %v", bundle["type"])
	}
}

func TestMetadataReflectsCapabilitySnapshot(t *testing.T) {
	snapshot := registry.CapabilitySnapshot{
		FHIRVersion: "4.0.1",
		CompiledAt:  time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Resources: []registry.ResourceCapability{{
			ResourceType: "Patient",
			SearchParameters: []registry.SearchParameterInfo{{
				Code: "family",
				Type: "string",
			}},
		}},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:  &fakeResourceService{},
		SearchService:    &fakeSearchService{},
		CapabilitySource: fakeCapabilitySource{snapshot: snapshot},
		ServerMetadata: hahttp.ServerMetadata{
			SoftwareName:    "haistack-http",
			SoftwareVersion: "0.1.0",
		},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cap map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &cap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cap["fhirVersion"] != "4.0.1" {
		t.Fatalf("fhirVersion = %v", cap["fhirVersion"])
	}
	rest, ok := cap["rest"].([]interface{})
	if !ok || len(rest) == 0 {
		t.Fatalf("rest = %v", cap["rest"])
	}
	restObj := rest[0].(map[string]interface{})
	resources := restObj["resource"].([]interface{})
	patient := resources[0].(map[string]interface{})
	if patient["type"] != "Patient" {
		t.Fatalf("type = %v", patient["type"])
	}
	interactions := patient["interaction"].([]interface{})
	if len(interactions) < 6 {
		t.Fatalf("interactions = %v", interactions)
	}
}

func TestMetadataOmitsSearchInteractionWithoutSearchService(t *testing.T) {
	snapshot := registry.CapabilitySnapshot{
		FHIRVersion: "4.0.1",
		CompiledAt:  time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Resources: []registry.ResourceCapability{{
			ResourceType: "Patient",
		}},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:  &fakeResourceService{},
		CapabilitySource: fakeCapabilitySource{snapshot: snapshot},
	})

	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cap map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &cap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rest := cap["rest"].([]interface{})
	patient := rest[0].(map[string]interface{})["resource"].([]interface{})[0].(map[string]interface{})
	interactions := patient["interaction"].([]interface{})
	for _, raw := range interactions {
		if raw.(map[string]interface{})["code"] == "search-type" {
			t.Fatalf("unexpected search interaction: %v", interactions)
		}
	}
}

func TestTransactionBundleOnly(t *testing.T) {
	var called bool
	svc := &fakeResourceService{
		transaction: func(_ context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
			called = true
			return &types.ResourceEnvelope{
				ResourceType: "Bundle",
				JSON:         []byte(`{"resourceType":"Bundle","type":"transaction-response","entry":[]}`),
			}, nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})

	txnBody := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","name":[{"family":"Doe"}]}}]}`)
	rec := doRequest(t, handler, http.MethodPost, "/fhir", txnBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected transaction handler to be called")
	}

	batchBody := []byte(`{"resourceType":"Bundle","type":"batch","entry":[]}`)
	rec = doRequest(t, handler, http.MethodPost, "/fhir", batchBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if !strings.Contains(outcome.Issue[0].Diagnostics, "transaction") {
		t.Fatalf("diagnostics = %q", outcome.Issue[0].Diagnostics)
	}
}

func TestTransactionBundleRequiresAuthorizationWhenConfigured(t *testing.T) {
	checker := &recordingAuthChecker{allow: false}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "user-1"}, auth.TenantContext{TenantID: "t1"}, nil
		},
		AuthChecker: checker,
	})

	rec := doRequest(t, handler, http.MethodPost, "/fhir", []byte(`{"resourceType":"Bundle","type":"transaction","entry":[]}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(checker.writeCalls) != 1 || checker.writeCalls[0] != "transaction:Bundle/" {
		t.Fatalf("write calls = %v", checker.writeCalls)
	}
}

func TestUnsupportedMethodReturnsOperationOutcome(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodPatch, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if outcome.Issue[0].Code != "not-supported" {
		t.Fatalf("code = %q", outcome.Issue[0].Code)
	}
}

func TestMalformedID(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/!!!invalid!!!", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBasePathRequiresSegmentBoundary(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, handler, http.MethodGet, "/fhirx/Patient/pat-1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthPassThroughWhenNotConfigured(t *testing.T) {
	svc := &fakeResourceService{
		readFn: func(_ context.Context, _, id string) (*types.ResourceEnvelope, error) {
			return patientEnvelope(id, "Doe"), nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuthUnauthenticated(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{}, auth.TenantContext{}, fmt.Errorf("no credentials")
		},
		AuthChecker: &recordingAuthChecker{allow: true},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if outcome.Issue[0].Code != "security" {
		t.Fatalf("code = %q", outcome.Issue[0].Code)
	}
}

func TestAuthDeniedReadWriteSearch(t *testing.T) {
	checker := &recordingAuthChecker{allow: false}
	resolver := func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
		return auth.Principal{ID: "user-1"}, auth.TenantContext{TenantID: "t1"}, nil
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &fakeResourceService{},
		SearchService:     &fakeSearchService{},
		PrincipalResolver: resolver,
		AuthChecker:       checker,
	})

	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read status = %d", rec.Code)
	}
	rec = doRequest(t, handler, http.MethodPost, "/fhir/Patient", patientJSON("", "Doe"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("write status = %d", rec.Code)
	}
	rec = doRequest(t, handler, http.MethodGet, "/fhir/Patient?family=Doe", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("search status = %d", rec.Code)
	}
}

func TestDeleteUsesDeleteAuthorizationOperation(t *testing.T) {
	checker := &recordingAuthChecker{allow: true}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			deleteFn: func(_ context.Context, _, _ string) error { return nil },
		},
		PrincipalResolver: func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
			return auth.Principal{ID: "user-1"}, auth.TenantContext{TenantID: "t1"}, nil
		},
		AuthChecker: checker,
	})

	rec := doRequest(t, handler, http.MethodDelete, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(checker.writeCalls) != 1 || checker.writeCalls[0] != "delete:Patient/pat-1" {
		t.Fatalf("write calls = %v", checker.writeCalls)
	}
}

func TestAuthAllowedWhenConfigured(t *testing.T) {
	checker := &recordingAuthChecker{allow: true}
	resolver := func(_ context.Context, _ *http.Request) (auth.Principal, auth.TenantContext, error) {
		return auth.Principal{ID: "user-1"}, auth.TenantContext{TenantID: "t1"}, nil
	}
	svc := &fakeResourceService{
		readFn: func(_ context.Context, _, id string) (*types.ResourceEnvelope, error) {
			return patientEnvelope(id, "Doe"), nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   svc,
		PrincipalResolver: resolver,
		AuthChecker:       checker,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(checker.readCalls) != 1 {
		t.Fatalf("read calls = %v", checker.readCalls)
	}
}

func TestSearchErrorMapping(t *testing.T) {
	searchSvc := &fakeSearchService{
		searchFn: func(_ context.Context, _ string, _ url.Values) (*search.SearchBundle, error) {
			return nil, search.ErrInvalidQuery
		},
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		SearchService:   searchSvc,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient?family=", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	outcome := decodeOutcome(t, rec.Body.Bytes())
	if outcome.Issue[0].Code != "invalid" {
		t.Fatalf("code = %q", outcome.Issue[0].Code)
	}
}

func TestNewHandlerRequiresResourceService(t *testing.T) {
	_, err := hahttp.NewHandler(hahttp.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}
