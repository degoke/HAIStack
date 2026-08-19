package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
)

func TestHealthEndpoints(t *testing.T) {
	ready := false
	handler := hahttp.WithHealthEndpoints(nil, func() bool { return ready })

	checks := []struct {
		path   string
		status int
	}{
		{path: "/health", status: http.StatusOK},
		{path: "/healthz", status: http.StatusOK},
		{path: "/readyz", status: http.StatusServiceUnavailable},
	}
	for _, check := range checks {
		req := httptest.NewRequest(http.MethodGet, check.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != check.status {
			t.Fatalf("GET %s status = %d, want %d", check.path, rec.Code, check.status)
		}
	}

	ready = true
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready endpoint status = %d, want %d", rec.Code, http.StatusOK)
	}
}
