package host_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/smart/host"
)

func TestHandler_WellKnownAndOAuthStub(t *testing.T) {
	fhir := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h, err := host.NewHandler(fhir, host.Config{
		FHIRBasePath:  "http://example.com/fhir",
		OAuthBasePath: "http://example.com/oauth",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fhir/.well-known/smart-configuration", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("well-known status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["authorization_endpoint"] != "http://example.com/oauth/authorize" {
		t.Fatalf("authorize = %v", cfg["authorization_endpoint"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/authorize?redirect_uri=http://app/cb&state=abc", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "http://app/cb?code=inferno-demo-code&state=abc" {
		t.Fatalf("location = %q", loc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("token status = %d", rec.Code)
	}
}
