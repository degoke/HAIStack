package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestMultiTenantServerRoutes(t *testing.T) {
	base, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	registry := oauth.NewTenantRegistry()
	issuer := "http://127.0.0.1:8080"
	tenantIssuer := issuer + "/t/local"
	if err := registry.Register(oauth.TenantIssuerConfig{
		TenantID:     "local",
		Issuer:       tenantIssuer,
		FHIRAudience: issuer,
		SigningKey:   base,
		AutoApprove:  boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	multi, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Issuer:       issuer,
			FHIRAudience: issuer,
			SigningKey:   base,
			AutoApprove:  true,
		},
		Tenants: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := oauth.CombineHandlers(nil, multi.Handler())

	req := httptest.NewRequest(http.MethodGet, "/t/local/.well-known/smart-configuration", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), tenantIssuer) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestMultiTenantServerIsolatesClients(t *testing.T) {
	base, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	clients := oauth.NewClientStore()
	if err := clients.Register(oauth.Client{
		ClientID:     "base-only",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	}); err != nil {
		t.Fatal(err)
	}
	registry := oauth.NewTenantRegistry()
	issuer := "http://127.0.0.1:8080"
	if err := registry.Register(oauth.TenantIssuerConfig{
		TenantID:     "local",
		Issuer:       issuer + "/t/local",
		FHIRAudience: issuer,
		SigningKey:   base,
		AutoApprove:  boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}
	multi, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Issuer:       issuer,
			FHIRAudience: issuer,
			SigningKey:   base,
			Clients:      clients,
			AutoApprove:  true,
		},
		Tenants: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := oauth.CombineHandlers(nil, multi.Handler())
	req := httptest.NewRequest(http.MethodGet, "/t/local/oauth/authorize?client_id=base-only&redirect_uri=https://localhost/callback&response_type=code&scope=patient/Patient.rs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func boolPtr(v bool) *bool {
	return &v
}
