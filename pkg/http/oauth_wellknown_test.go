package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
)

func TestRootHandlerMirrorsOAuthWellKnownUnderFHIR(t *testing.T) {
	oauthHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/smart-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_endpoint":"https://example.com/oauth/authorize"}`))
	})
	root := hahttp.NewRootHandlerFromConfig(hahttp.RootConfig{
		FHIR:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		OAuth: oauthHandler,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fhir/.well-known/smart-configuration", nil)
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}
