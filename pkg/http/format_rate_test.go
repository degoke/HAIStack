package http_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"testing"
	"time"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestFHIRXMLNegotiationAndRequestParsing(t *testing.T) {
	svc := &fakeResourceService{
		readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
			return patientEnvelope(id, "Doe"), nil
		},
		createFn: func(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
			if resource.ID != "true" {
				t.Fatalf("XML id = %q, want true", resource.ID)
			}
			resource.ID = "xml-1"
			return patientEnvelope("xml-1", "Doe"), nil
		},
	}
	handler := newTestHandler(t, hahttp.Config{ResourceService: svc})

	read := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil, map[string]string{
		"Accept": "application/fhir+xml",
	})
	if read.Code != http.StatusOK || read.Header().Get("Content-Type") != "application/fhir+xml" {
		t.Fatalf("read response = %d %q %s", read.Code, read.Header().Get("Content-Type"), read.Body.String())
	}
	var root struct {
		XMLName xml.Name
		ID      struct {
			Value string `xml:"value,attr"`
		} `xml:"id"`
	}
	if err := xml.Unmarshal(read.Body.Bytes(), &root); err != nil {
		t.Fatalf("decode FHIR XML: %v", err)
	}
	if root.XMLName.Local != "Patient" || root.ID.Value != "pat-1" {
		t.Fatalf("XML root/id = %q/%q", root.XMLName.Local, root.ID.Value)
	}

	created := doRequestWithHeaders(t, handler, http.MethodPost, "/fhir/Patient", []byte(`<Patient xmlns="http://hl7.org/fhir"><id value="true"/><name><family value="Doe"/></name></Patient>`), map[string]string{
		"Content-Type": "application/fhir+xml",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("XML create status = %d, body = %s", created.Code, created.Body.String())
	}

	wildcard := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil, map[string]string{
		"Accept": "application/fhir+json;q=0, */*;q=0.5",
	})
	if wildcard.Code != http.StatusOK || wildcard.Header().Get("Content-Type") != "application/fhir+xml" {
		t.Fatalf("Accept precedence = %d %q", wildcard.Code, wildcard.Header().Get("Content-Type"))
	}

	none := doRequestWithHeaders(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil, map[string]string{
		"Accept": "application/fhir+json;q=0, application/fhir+xml;q=0",
	})
	if none.Code != http.StatusNotAcceptable {
		t.Fatalf("all-zero Accept status = %d", none.Code)
	}
}

func TestRateLimitMiddlewareReturnsStructured429(t *testing.T) {
	handler := newTestHandler(t, hahttp.Config{
		ResourceService: &fakeResourceService{
			readFn: func(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
				return patientEnvelope(id, "Doe"), nil
			},
		},
		RateLimit: hahttp.RateLimitConfig{
			Requests: 1,
			Window:   time.Minute,
			Key:      func(_ *http.Request) string { return "test-client" },
		},
	})

	first := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := doRequest(t, handler, http.MethodGet, "/fhir/Patient/pat-1", nil)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" || second.Header().Get("X-RateLimit-Limit") != "1" {
		t.Fatalf("rate headers = %#v", second.Header())
	}
	outcome := decodeOutcome(t, second.Body.Bytes())
	if len(outcome.Issue) == 0 || outcome.Issue[0].Code != "throttled" {
		t.Fatalf("outcome = %+v", outcome)
	}
}
