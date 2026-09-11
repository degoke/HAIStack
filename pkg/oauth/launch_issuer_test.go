package oauth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestLaunchIssuerRegistryRotatingSecrets(t *testing.T) {
	reg := oauth.NewLaunchIssuerRegistry()
	if err := reg.RegisterRotating("ehr-launcher", "old-secret", "new-secret"); err != nil {
		t.Fatal(err)
	}
	if !reg.Validate("ehr-launcher", "old-secret") {
		t.Fatal("old secret should remain valid during rotation")
	}
	if !reg.Validate("ehr-launcher", "new-secret") {
		t.Fatal("new secret should be valid")
	}
	if reg.Validate("ehr-launcher", "wrong") {
		t.Fatal("unexpected secret accepted")
	}
}

func TestLaunchIssuerRegistryReplacesSingleSecret(t *testing.T) {
	reg := oauth.NewLaunchIssuerRegistry()
	_ = reg.Register("ehr-launcher", "secret-a")
	_ = reg.Register("ehr-launcher", "secret-b")
	if reg.Validate("ehr-launcher", "secret-a") {
		t.Fatal("previous secret should not remain valid after Register")
	}
	if !reg.Validate("ehr-launcher", "secret-b") {
		t.Fatal("latest secret should be valid")
	}
}

func TestLaunchEndpointAcceptsRotatingCredential(t *testing.T) {
	reg := oauth.NewLaunchIssuerRegistry()
	if err := reg.RegisterRotating("ehr-launcher", "ehr-secret", "ehr-secret-next"); err != nil {
		t.Fatal(err)
	}
	ts := newLaunchTestServer(t, reg)
	defer ts.Close()
	for _, secret := range []string{"ehr-secret", "ehr-secret-next"} {
		values := url.Values{}
		values.Set("launch_issuer_id", "ehr-launcher")
		values.Set("launch_issuer_secret", secret)
		values.Set("patient", "patient-rotating")
		resp, err := http.Post(ts.URL+"/oauth/launch", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("secret %q status = %d", secret, resp.StatusCode)
		}
	}
}

func TestUsesInMemoryStoresDefaultsTrue(t *testing.T) {
	if !oauth.UsesInMemoryStores(oauth.Config{}) {
		t.Fatal("expected default config to use in-memory stores")
	}
}

func TestConsentShowsClientName(t *testing.T) {
	autoApprove := false
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			ClientName:   "Demo SMART App",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read"},
		},
	})
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
		AutoApprove: &autoApprove,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=abc&code_challenge_method=S256"
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Demo SMART App") {
		t.Fatalf("body = %s", body)
	}
}

func TestConsentLoginRequired(t *testing.T) {
	autoApprove := false
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read"},
		},
	})
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:       "http://example.test",
		FHIRBaseURL:  "http://example.test/fhir",
		Signer:       oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:      reg,
		AutoApprove:  &autoApprove,
		ConsentLogin: stubConsentLogin{loggedIn: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=abc&code_challenge_method=S256")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMultiTenantConsentBranding(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read"},
		},
	})
	tenants := oauth.NewTenantRegistry()
	autoApprove := false
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "http://example.test/t/tenant-a",
		FHIRBaseURL: "http://example.test/t/tenant-a/fhir",
		ConsentUI: &oauth.ConsentUIConfig{
			Title: "Tenant A Authorization",
		},
		AutoApprove: &autoApprove,
	})
	mts, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base: oauth.Config{
			Signer:  oauth.RS256Signer{PrivateKey: key, Kid: "test"},
			Clients: reg,
		},
		Tenants: tenants,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mts.Handler())
	defer ts.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authURL := ts.URL + "/t/tenant-a/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=abc&code_challenge_method=S256"
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Tenant A Authorization") {
		t.Fatalf("body = %s", body)
	}
}

type stubConsentLogin struct {
	loggedIn bool
}

func (s stubConsentLogin) EnsureLoggedIn(w http.ResponseWriter, _ *http.Request, _ string) (string, bool) {
	if s.loggedIn {
		return "user-1", true
	}
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "login required")
	return "", false
}

func newLaunchTestServer(t *testing.T, reg *oauth.LaunchIssuerRegistry) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	regCopy := reg
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:        "http://example.test",
		FHIRBaseURL:   "http://example.test/fhir",
		Signer:        oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:       oauth.NewClientRegistry(),
		LaunchIssuers: regCopy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Handler())
}
