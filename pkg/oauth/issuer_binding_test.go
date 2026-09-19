package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestIssuerBinding_RejectsCrossIssuerAuthorizationCode(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	issuerA := "https://auth.example/t/clinic-a"
	issuerB := "https://auth.example/t/clinic-b"
	now := time.Now()

	code := "shared-code-test"
	if err := store.SaveAuthorizationCode(code, oauth.AuthorizationCode{
		Issuer:      issuerA,
		ClientID:    "client",
		RedirectURI: "https://app/cb",
		Scope:       "openid",
		ExpiresAt:   now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.ConsumeAuthorizationCode(issuerB, code); ok {
		t.Fatal("expected cross-issuer code consume to fail")
	}
	entry, ok := store.ConsumeAuthorizationCode(issuerA, code)
	if !ok || entry.ClientID != "client" {
		t.Fatalf("expected code for issuer A: ok=%v entry=%+v", ok, entry)
	}
}

func TestIssuerBinding_MultiTenantTokenExchange(t *testing.T) {
	store := oauth.NewMemoryAuthorizationStore()
	base := "https://auth.example"
	issuerA := base + "/t/clinic-a"
	issuerB := base + "/t/clinic-b"

	key, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	registry := oauth.NewTenantRegistry()
	for _, iss := range []string{issuerA, issuerB} {
		if err := registry.Register(oauth.TenantIssuerConfig{
			TenantID:     strings.TrimPrefix(iss, base+"/t/"),
			Issuer:       iss,
			FHIRAudience: base,
		}); err != nil {
			t.Fatal(err)
		}
	}
	multi, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			SigningKey:         key,
			AuthorizationStore: store,
			AutoApprove:        true,
		},
		Tenants: registry,
	})
	if err != nil {
		t.Fatal(err)
	}

	srvA, err := multi.ServerForTenant("clinic-a")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "app"
	clientSecret := "secret"
	if err := srvA.RegisterClient(oauth.Client{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://app/cb"},
		Scopes:                  []string{"openid", "patient/Patient.rs"},
	}); err != nil {
		t.Fatal(err)
	}
	code := "tenant-a-code"
	_ = store.SaveAuthorizationCode(code, oauth.AuthorizationCode{
		Issuer:      issuerA,
		ClientID:    clientID,
		RedirectURI: "https://app/cb",
		Scope:       "openid patient/Patient.rs",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	srvB, err := multi.ServerForTenant("clinic-b")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://app/cb"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srvB.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tenant B token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	srvA.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("tenant A token status = %d body=%s", rec2.Code, rec2.Body.String())
	}
}
