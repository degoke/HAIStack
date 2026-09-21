package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/export"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestVReadAndHistorySince(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	svc := &fakeResourceService{
		historyFn: func(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
			return []store.ResourceVersion{
				{
					ResourceType: resourceType, ID: id, VersionID: "v1",
					Action: store.VersionActionCreate, Timestamp: t1,
					Resource: patientEnvelope(id, "Doe"),
				},
				{
					ResourceType: resourceType, ID: id, VersionID: "v2",
					Action: store.VersionActionUpdate, Timestamp: t2,
					Resource: patientEnvelope(id, "Smith"),
				},
			}, nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})

	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/_history/v1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("vread status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Doe"`) {
		t.Fatalf("vread body=%s", rec.Body.String())
	}

	rec = doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/_history?_since=2024-03-01T00:00:00Z", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", rec.Code, rec.Body.String())
	}
	var bundle map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	entries := bundle["entry"].([]any)
	if len(entries) != 1 {
		t.Fatalf("since entries=%d", len(entries))
	}
}

func TestFHIRPatchHTTP(t *testing.T) {
	var got []byte
	svc := &fakeResourceService{
		patchFn: func(_ context.Context, _, _ string, patchJSON []byte) (*types.ResourceEnvelope, error) {
			got = patchJSON
			return patientEnvelope("pat-1", "Patched"), nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})
	body := []byte(`{"resourceType":"Parameters","parameter":[{"name":"operation","part":[{"name":"type","valueCode":"replace"},{"name":"path","valueString":"Patient.gender"},{"name":"value","valueCode":"male"}]}]}`)
	req := httptest.NewRequest(http.MethodPatch, "/fhir/Patient/pat-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/fhir+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(got, []byte("Parameters")) {
		t.Fatalf("expected FHIR Patch body, got %s", got)
	}
}

func TestConceptMapTranslateHTTP(t *testing.T) {
	ctx := context.Background()
	mem := terminology.NewMemoryStore()
	raw := []byte(`{"resourceType":"ConceptMap","url":"http://example.org/maps/gender","group":[{"element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]}`)
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
		CanonicalURL: "http://example.org/maps/gender", ResourceJSON: raw,
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:    &fakeResourceService{},
		TerminologyService: terminology.NewLocalService(mem, "default"),
		TerminologyScope:   "default",
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/ConceptMap/$translate?url=http://example.org/maps/gender&code=F", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"female"`) {
		t.Fatalf("translate body=%s", rec.Body.String())
	}
}

func TestPatientExportKickoff(t *testing.T) {
	svc := newBulkExportService(t)
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:   &bulkResourceService{},
		BulkExportService: svc,
	})
	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient/p1/$export", nil)
	req.Header.Set("Prefer", "respond-async")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetadataOmitsUnimplementedPackageAndAdvertisesVRead(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		CapabilitySource: fakeCapabilitySource{snapshot: registry.CapabilitySnapshot{
			FHIRVersion: "4.0.1",
			Resources:   []registry.ResourceCapability{{ResourceType: "Patient"}},
		}},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"name":"package"`) {
		t.Fatal("metadata must not advertise unimplemented $package")
	}
	if !strings.Contains(body, `"vread"`) {
		t.Fatal("metadata missing vread")
	}
	if strings.Contains(body, `"everything"`) {
		t.Fatal("metadata must not advertise $everything without an Everything implementation")
	}
	if strings.Contains(body, `"lookup"`) {
		t.Fatal("metadata must not advertise terminology ops without a terminology service")
	}
	if strings.Contains(body, `"translate"`) {
		t.Fatal("metadata must not advertise $translate without a Translate implementation")
	}
}

func TestMetadataAdvertisesEverythingWhenImplemented(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: hahttp.CoreResourceService{Svc: mustCoreSQLiteService(t)},
		CapabilitySource: fakeCapabilitySource{snapshot: registry.CapabilitySnapshot{
			FHIRVersion: "4.0.1",
			Resources:   []registry.ResourceCapability{{ResourceType: "Patient"}},
		}},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"everything"`) {
		t.Fatal("metadata missing $everything")
	}
}

func TestMetadataAdvertisesTranslateForChain(t *testing.T) {
	ctx := context.Background()
	mem := terminology.NewMemoryStore()
	raw := []byte(`{"resourceType":"ConceptMap","url":"http://example.org/maps/gender","group":[{"element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]}`)
	if err := mem.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
		CanonicalURL: "http://example.org/maps/gender", ResourceJSON: raw,
	}); err != nil {
		t.Fatal(err)
	}
	chain := terminology.Chain{Providers: []terminology.Provider{terminology.NewLocalService(mem, "default")}}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService:    &fakeResourceService{},
		TerminologyService: chain,
		CapabilitySource: fakeCapabilitySource{snapshot: registry.CapabilitySnapshot{
			FHIRVersion: "4.0.1",
			Resources:   []registry.ResourceCapability{{ResourceType: "ConceptMap"}},
		}},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"translate"`) {
		t.Fatal("metadata missing $translate for Chain")
	}
	rec = doRequest(t, handler, http.MethodGet, "/fhir/ConceptMap/$translate?url=http://example.org/maps/gender&code=F", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("translate via Chain status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFHIRPatchRejectsXMLContentType(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	req := httptest.NewRequest(http.MethodPatch, "/fhir/Patient/pat-1", strings.NewReader("<Parameters/>"))
	req.Header.Set("Content-Type", "application/fhir+xml")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEverythingUsesCoreService(t *testing.T) {
	mem := coreEverythingService(t)
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: hahttp.CoreResourceService{Svc: mem},
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/$everything", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Patient"`) {
		t.Fatalf("everything body=%s", rec.Body.String())
	}
}

func coreEverythingService(t *testing.T) *core.ResourceService {
	t.Helper()
	ctx := context.Background()
	harness := newHTTPCoreHarness(t)
	if _, err := harness.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatal(err)
	}
	return harness
}

func newHTTPCoreHarness(t *testing.T) *core.ResourceService {
	t.Helper()
	// Reuse sqlite integration stack would be heavy; use core test via export memory?
	// Core ResourceService needs stores. Use the export memoryResources wrapped? No HistoryStore.
	// Use Core from sqlite-less mem via creating through core_test harness is in another package.
	// Build a tiny in-http-test store using sqlite Open in tempdir is already used in integration tests.
	return mustCoreSQLiteService(t)
}

func mustCoreSQLiteService(t *testing.T) *core.ResourceService {
	t.Helper()
	handler, svc := openIntegrationStack(t)
	_ = handler
	return svc
}

func TestExportPatientIDResolvesCompartment(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resources := &memoryExportResources{byType: map[string]map[string]*types.ResourceEnvelope{
		"Patient": {
			"p1": {ResourceType: "Patient", ID: "p1", LastUpdated: now, JSON: []byte(`{"resourceType":"Patient","id":"p1"}`)},
			"p2": {ResourceType: "Patient", ID: "p2", LastUpdated: now, JSON: []byte(`{"resourceType":"Patient","id":"p2"}`)},
		},
		"Observation": {
			"o1": {ResourceType: "Observation", ID: "o1", LastUpdated: now, JSON: []byte(`{"resourceType":"Observation","id":"o1","subject":{"reference":"Patient/p1"}}`)},
			"o2": {ResourceType: "Observation", ID: "o2", LastUpdated: now, JSON: []byte(`{"resourceType":"Observation","id":"o2","subject":{"reference":"Patient/p2"}}`)},
		},
	}}
	files := export.NewInMemoryFileStore()
	jobs := export.NewInMemoryJobStore()
	svc, err := export.NewService(export.Config{
		Jobs: jobs, Files: files,
		Executor: &export.Executor{Resources: resources, Files: files},
		Now:      func() time.Time { return now },
		NewID:    func() string { return "job-p" },
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := svc.Kickoff(context.Background(), export.KickoffRequest{
		ResourceTypes: []string{"Patient", "Observation"},
		PatientID:     "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != export.StatusComplete {
		t.Fatalf("status=%s err=%s", job.Status, job.LastError)
	}
	data, _, err := svc.GetFile(context.Background(), job.ID, "Observation.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "p2") {
		t.Fatalf("exported other patient: %s", data)
	}
}

type memoryExportResources struct {
	byType map[string]map[string]*types.ResourceEnvelope
}

func (m *memoryExportResources) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	items := m.byType[resourceType]
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	return ids[offset:end], nil
}

func (m *memoryExportResources) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env := m.byType[resourceType][id]; env != nil {
		return env, nil
	}
	return nil, context.Canceled
}
