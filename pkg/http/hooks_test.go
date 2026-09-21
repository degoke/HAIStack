package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/hooks"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestIncomingHookRejectsRequest(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.Incoming, func(_ context.Context, event *hooks.Event) error {
		if event.Action == hooks.ActionRead && event.ResourceType == "Patient" {
			return errors.New("no patients")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(context.Context, string, string) (*types.ResourceEnvelope, error) {
				t.Fatal("read should not run")
				return nil, nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOutgoingHookMutatesResponse(t *testing.T) {
	reg := hooks.NewRegistry()
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		if event.Resource == nil {
			return errors.New("missing resource")
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Resource.JSON, &payload); err != nil {
			return err
		}
		payload["active"] = true
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		event.Resource.JSON = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, _, id string) (*types.ResourceEnvelope, error) {
				return patientEnvelope(id, "Doe"), nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"active":true`) {
		t.Fatalf("outgoing mutation missing: %s", rec.Body.String())
	}
}

func TestOutgoingHookRunsOnSearchBundle(t *testing.T) {
	reg := hooks.NewRegistry()
	var seenAction hooks.Action
	var seenType string
	var seenEnvelopeType string
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		seenAction = event.Action
		seenType = event.ResourceType
		if event.Resource == nil {
			return errors.New("missing search bundle")
		}
		seenEnvelopeType = event.Resource.ResourceType
		event.Resource.JSON = []byte(strings.ReplaceAll(string(event.Resource.JSON), `"Doe"`, `"REDACTED"`))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	total := 1
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		SearchService: &fakeSearchService{
			searchFn: func(_ context.Context, resourceType string, _ url.Values) (*search.SearchBundle, error) {
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
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient?family=Doe", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if seenAction != hooks.ActionSearch {
		t.Fatalf("outgoing action = %q, want search", seenAction)
	}
	if seenType != "Patient" {
		t.Fatalf("outgoing resourceType = %q, want Patient (query type, not Bundle)", seenType)
	}
	if seenEnvelopeType != "Bundle" {
		t.Fatalf("outgoing resource envelope type = %q, want Bundle", seenEnvelopeType)
	}
	if !strings.Contains(rec.Body.String(), "REDACTED") {
		t.Fatalf("expected PHI-stripped search bundle, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"Doe"`) {
		t.Fatalf("original family still present: %s", rec.Body.String())
	}
}

func TestOutgoingHookRunsOnHistoryBundle(t *testing.T) {
	reg := hooks.NewRegistry()
	var seenAction hooks.Action
	var seenType string
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		seenAction = event.Action
		seenType = event.ResourceType
		if event.Resource == nil || event.Resource.ResourceType != "Bundle" {
			return errors.New("expected history bundle envelope")
		}
		event.Resource.JSON = []byte(strings.ReplaceAll(string(event.Resource.JSON), `"Doe"`, `"REDACTED"`))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			historyFn: func(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
				return []store.ResourceVersion{{
					ResourceType: resourceType,
					ID:           id,
					VersionID:    "1",
					Action:       store.VersionActionCreate,
					Resource:     patientEnvelope(id, "Doe"),
				}}, nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/_history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if seenAction != hooks.ActionHistory {
		t.Fatalf("outgoing action = %q, want history", seenAction)
	}
	if seenType != "Patient" {
		t.Fatalf("outgoing resourceType = %q, want Patient (query type, not Bundle)", seenType)
	}
	if !strings.Contains(rec.Body.String(), "REDACTED") {
		t.Fatalf("expected PHI-stripped history bundle, got %s", rec.Body.String())
	}
}

func TestOutgoingHookRunsOnEverythingBundle(t *testing.T) {
	reg := hooks.NewRegistry()
	var seenAction hooks.Action
	var seenType, seenOp, seenEnvelopeType string
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		seenAction = event.Action
		seenType = event.ResourceType
		seenOp = event.Operation
		if event.Resource == nil {
			return errors.New("missing $everything bundle")
		}
		seenEnvelopeType = event.Resource.ResourceType
		event.Resource.JSON = []byte(strings.ReplaceAll(string(event.Resource.JSON), `"Doe"`, `"REDACTED"`))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
				if resourceType == "Patient" && id == "pat-1" {
					return patientEnvelope(id, "Doe"), nil
				}
				return nil, errors.New("not found")
			},
		},
		SearchService: &fakeSearchService{
			searchFn: func(_ context.Context, resourceType string, _ url.Values) (*search.SearchBundle, error) {
				return &search.SearchBundle{ResourceType: resourceType}, nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1/$everything?_type=Observation", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if seenAction != hooks.ActionOperation {
		t.Fatalf("outgoing action = %q, want operation", seenAction)
	}
	if seenType != "Patient" {
		t.Fatalf("outgoing resourceType = %q, want Patient (query type, not Bundle)", seenType)
	}
	if seenOp != "$everything" {
		t.Fatalf("outgoing operation = %q, want $everything", seenOp)
	}
	if seenEnvelopeType != "Bundle" {
		t.Fatalf("outgoing resource envelope type = %q, want Bundle", seenEnvelopeType)
	}
	if !strings.Contains(rec.Body.String(), "REDACTED") {
		t.Fatalf("expected PHI-stripped $everything bundle, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"Doe"`) {
		t.Fatalf("original family still present: %s", rec.Body.String())
	}
}

func TestOutgoingHookRunsOnMetadata(t *testing.T) {
	reg := hooks.NewRegistry()
	var seenAction hooks.Action
	var seenType string
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		seenAction = event.Action
		seenType = event.ResourceType
		if event.Resource == nil {
			return errors.New("missing capability statement")
		}
		event.Resource.JSON = []byte(strings.ReplaceAll(string(event.Resource.JSON), `"haistack-http"`, `"REDACTED"`))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{},
		CapabilitySource: fakeCapabilitySource{snapshot: registry.CapabilitySnapshot{
			FHIRVersion: "4.0.1",
			Resources:   []registry.ResourceCapability{{ResourceType: "Patient"}},
		}},
		ServerMetadata: hahttp.ServerMetadata{SoftwareName: "haistack-http"},
		Hooks:          reg,
	})
	rec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if seenAction != hooks.ActionMetadata {
		t.Fatalf("outgoing action = %q, want metadata", seenAction)
	}
	if seenType != "CapabilityStatement" {
		t.Fatalf("outgoing resourceType = %q, want CapabilityStatement", seenType)
	}
	if !strings.Contains(rec.Body.String(), "REDACTED") {
		t.Fatalf("expected redacted metadata, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"haistack-http"`) {
		t.Fatalf("original software name still present: %s", rec.Body.String())
	}
}

func TestBundleHooksUseBatchAction(t *testing.T) {
	reg := hooks.NewRegistry()
	var incoming, outgoing hooks.Action
	if err := reg.On(hooks.Incoming, func(_ context.Context, event *hooks.Event) error {
		incoming = event.Action
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.On(hooks.Outgoing, func(_ context.Context, event *hooks.Event) error {
		outgoing = event.Action
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			batch: func(_ context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
				return &types.ResourceEnvelope{
					ResourceType: "Bundle",
					JSON:         []byte(`{"resourceType":"Bundle","type":"batch-response","entry":[]}`),
				}, nil
			},
		},
		Hooks: reg,
	})
	rec := doRequest(t, handler, http.MethodPost, "/fhir", []byte(`{"resourceType":"Bundle","type":"batch","entry":[]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if incoming != hooks.ActionBatch {
		t.Fatalf("incoming action = %q, want batch", incoming)
	}
	if outgoing != hooks.ActionBatch {
		t.Fatalf("outgoing action = %q, want batch", outgoing)
	}
}
