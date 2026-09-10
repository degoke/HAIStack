package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestPKCE_S256Only(t *testing.T) {
	store := oauth.NewCodeStore()
	code, err := store.Issue(oauth.AuthCode{
		ClientID:            "app",
		RedirectURI:         "https://app/cb",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Exchange(code, "app", "https://app/cb", "verifier")
	if err == nil {
		t.Fatal("expected plain PKCE to be rejected")
	}
}

func TestAuthCodeFlow_PKCE(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	if err := reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/Patient.read", "launch/patient"},
		},
		DefaultPatient: "patient-123",
		DefaultUser:    "Practitioner/demo",
		TenantHint:     "tenant-a",
	}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	issuer := "http://" + listener.Addr().String()
	fhirBase := issuer + "/fhir"
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      issuer,
		FHIRBaseURL: fhirBase,
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
		TokenTTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener = listener
	ts.Start()
	defer ts.Close()

	smartClient, err := client.New(client.Config{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := smartClient.SMART().Discover(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TokenEndpoint == "" || cfg.AuthorizationEndpoint == "" {
		t.Fatalf("discovery = %#v", cfg)
	}

	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := smartClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:    "standalone-app",
		RedirectURI: "https://app.example/callback",
		Scope:       "patient/Patient.read launch/patient",
		State:       "state-1",
		PKCE:        pkce,
		Aud:         fhirBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d body = %s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "code=") {
		t.Fatalf("redirect = %q", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatal("missing code")
	}
	if u.Query().Get("state") != "state-1" {
		t.Fatalf("state = %q", u.Query().Get("state"))
	}

	tokenResp, err := smartClient.SMART().ExchangeAuthCode(context.Background(), cfg.TokenEndpoint, "standalone-app", "https://app.example/callback", code, pkce)
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}
	if tokenResp.Patient != "patient-123" {
		t.Fatalf("patient = %q", tokenResp.Patient)
	}

	wired, err := oauth.WireHTTP(oauth.WireConfig{
		Server: srv,
		Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
			DefaultTenantID:  "tenant-a",
			DefaultUserRoles: []string{"clinician"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient/x", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	principal, tenant, err := wired.PrincipalResolver(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if tenant.PatientScope != "patient-123" {
		t.Fatalf("patient scope = %q", tenant.PatientScope)
	}
	if tenant.TenantID != "tenant-a" {
		t.Fatalf("tenant = %q", tenant.TenantID)
	}
	if principal.ID == "" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestAuthCode_Expired(t *testing.T) {
	now := time.Now()
	store := oauth.NewCodeStore()
	store.Now = func() time.Time { return now }
	code, err := store.Issue(oauth.AuthCode{
		ClientID:    "app",
		RedirectURI: "https://app/cb",
		ExpiresAt:   now.Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Exchange(code, "app", "https://app/cb", "")
	if err == nil {
		t.Fatal("expected expired code error")
	}
}

func TestAuthCode_SingleUse(t *testing.T) {
	store := oauth.NewCodeStore()
	code, err := store.Issue(oauth.AuthCode{
		ClientID:    "app",
		RedirectURI: "https://app/cb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exchange(code, "app", "https://app/cb", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exchange(code, "app", "https://app/cb", ""); err == nil {
		t.Fatal("expected reuse to fail")
	}
}

func TestRedirectURI_ExactMatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"patient/*.read"},
		},
	})
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "https://example.com",
		FHIRBaseURL: "https://example.com/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key},
		Clients:     reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/oauth/authorize?response_type=code&client_id=app&redirect_uri=https://app.example/callback/evil&scope=patient/*.read")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Fatalf("expected error redirect, got %q", loc)
	}
}

func TestPolicyDenyOverridesScopes(t *testing.T) {
	engine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"Patient.read"},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [{
    "name": "deny-all",
    "effect": "deny",
    "match": { "actions": ["read"], "resourceTypes": ["Patient"] },
    "reason": "deny for test"
  }]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	checker := oauth.ScopePolicyAuthChecker{Engine: engine}
	decision, err := checker.AuthorizeRead(context.Background(), auth.Principal{ID: "u1", Kind: auth.KindUser}, auth.TenantContext{TenantID: "t1"}, "Patient", "x")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("expected policy deny")
	}
}

func TestSmartConfiguration(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	reg := oauth.NewClientRegistry()
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "https://example.com",
		FHIRBaseURL: "https://example.com/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key},
		Clients:     reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/.well-known/smart-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["token_endpoint"] == "" {
		t.Fatalf("doc = %#v", doc)
	}
	methods, _ := doc["code_challenge_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "S256" {
		t.Fatalf("pkce methods = %#v", methods)
	}
}

func TestHS256TokenRoundTrip(t *testing.T) {
	secret := []byte("test-secret-key-material")
	signer := oauth.HS256Signer{Secret: secret}
	token, _, err := oauth.IssueAccessToken(signer, "https://issuer", oauth.AccessTokenClaims{
		Subject:  "user-1",
		ClientID: "app",
		Scope:    "patient/*.read",
		Audience: "https://issuer/fhir",
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := smart.ValidateToken(token, signer.Verifier(), smart.TokenValidateOptions{
		ExpectedIssuer:   "https://issuer",
		ExpectedAudience: "https://issuer/fhir",
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims.Scope != "patient/*.read" {
		t.Fatalf("scope = %q", claims.Scope)
	}
}
