package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
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
		AutoApprove: demoAutoApprove(),
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
		AutoApprove: demoAutoApprove(),
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
	decision, err := checker.AuthorizeRead(context.Background(), auth.Principal{
		ID:         "u1",
		Kind:       auth.KindUser,
		Attributes: map[string]string{"smart.scope": "patient/Patient.read"},
	}, auth.TenantContext{TenantID: "t1"}, "Patient", "x")
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
		AutoApprove: demoAutoApprove(),
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

func TestRefreshTokenGrant_Rotation(t *testing.T) {
	key, srv, ts := newTestServer(t, testServerOpts{
		scopes: []string{"patient/Patient.read", "offline_access", "openid"},
	})
	defer ts.Close()
	_ = key

	tokenResp := exchangeAuthCode(t, ts.URL, srv, "offline_access openid patient/Patient.read")
	if tokenResp.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	if tokenResp.IDToken == "" {
		t.Fatal("expected id_token")
	}

	smartClient, err := client.New(client.Config{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := smartClient.SMART().RefreshToken(context.Background(), srv.TokenEndpoint(), "standalone-app", tokenResp.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	if refreshed.RefreshToken == tokenResp.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}
}

func TestRevokeAccessToken_BlocksResolver(t *testing.T) {
	key, srv, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()
	_ = key

	tokenResp := exchangeAuthCode(t, ts.URL, srv, "patient/Patient.read")
	jti, exp, err := oauth.ParseAccessTokenJTI(tokenResp.AccessToken)
	if err != nil || jti == "" {
		t.Fatalf("jti = %q err = %v", jti, err)
	}
	if err := srv.RevocationStore().Revoke(jti, "access_token", exp); err != nil {
		t.Fatal(err)
	}
	wired, err := oauth.WireHTTP(oauth.WireConfig{Server: srv})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/fhir/Patient/x", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	_, _, err = wired.PrincipalResolver(context.Background(), req)
	if err == nil {
		t.Fatal("expected revoked token to fail")
	}
}

func TestOpenIDConfiguration(t *testing.T) {
	_, _, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["issuer"] == "" || doc["revocation_endpoint"] == "" {
		t.Fatalf("doc = %#v", doc)
	}
	algs, _ := doc["id_token_signing_alg_values_supported"].([]any)
	if len(algs) == 0 {
		t.Fatalf("algs = %#v", algs)
	}
}

type testServerOpts struct {
	scopes      []string
	autoApprove *bool
}

func newTestServer(t *testing.T, opts testServerOpts) (*rsa.PrivateKey, *oauth.Server, *httptest.Server) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	scopes := opts.scopes
	if len(scopes) == 0 {
		scopes = []string{"patient/Patient.read", "launch/patient"}
	}
	if err := reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "standalone-app",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       scopes,
		},
		DefaultPatient: "patient-123",
		DefaultUser:    "Practitioner/demo",
		TenantHint:     "tenant-a",
	}); err != nil {
		t.Fatal(err)
	}
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID: "introspect-client",
			Scopes:   []string{"patient/Patient.read"},
		},
		Confidential: true,
		ClientSecret: "introspect-secret",
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://" + listener.Addr().String()
	autoApprove := demoAutoApprove()
	if opts.autoApprove != nil {
		autoApprove = opts.autoApprove
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      issuer,
		FHIRBaseURL: issuer + "/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
		AutoApprove: autoApprove,
		LaunchIssuerAuth: &oauth.LaunchIssuerAuth{
			ClientID:     "ehr-launcher",
			ClientSecret: "ehr-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener = listener
	ts.Start()
	return key, srv, ts
}

func exchangeAuthCode(t *testing.T, baseURL string, srv *oauth.Server, scope string) *client.TokenResponse {
	t.Helper()
	smartClient, err := client.New(client.Config{BaseURL: baseURL})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := smartClient.SMART().Discover(context.Background(), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := smartClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:    "standalone-app",
		RedirectURI: "https://app.example/callback",
		Scope:       scope,
		State:       "state-1",
		PKCE:        pkce,
		Aud:         srv.FHIRBaseURL(),
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
	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	return mustExchange(t, smartClient, cfg.TokenEndpoint, u.Query().Get("code"), pkce)
}

func mustExchange(t *testing.T, smartClient *client.Client, tokenEndpoint, code string, pkce *client.PKCEChallenge) *client.TokenResponse {
	t.Helper()
	tokenResp, err := smartClient.SMART().ExchangeAuthCode(context.Background(), tokenEndpoint, "standalone-app", "https://app.example/callback", code, pkce)
	if err != nil {
		t.Fatal(err)
	}
	return tokenResp
}

func demoAutoApprove() *bool {
	v := true
	return &v
}

func TestScopePolicyDeniesMissingScopes(t *testing.T) {
	engine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"Patient.read"},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [{
    "name": "allow-patient-read",
    "effect": "allow",
    "match": { "actions": ["read"], "resourceTypes": ["Patient"], "anyPermissions": ["Patient.read"] },
    "reason": "allow"
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
		t.Fatal("expected SMART scope deny")
	}
}

func TestConsentRequiredWhenAutoApproveDisabled(t *testing.T) {
	autoApprove := false
	_, _, ts := newTestServer(t, testServerOpts{autoApprove: &autoApprove})
	defer ts.Close()
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256"
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "csrf_token") {
		t.Fatalf("body = %s", body)
	}
}

func TestConsentBypassRejectedWithoutCSRFSession(t *testing.T) {
	autoApprove := false
	_, _, ts := newTestServer(t, testServerOpts{autoApprove: &autoApprove})
	defer ts.Close()
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256&approved=yes"
	resp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "csrf_token") {
		t.Fatalf("expected consent page, body = %s", body)
	}
}

func TestConsentApprovalIssuesCode(t *testing.T) {
	autoApprove := false
	_, _, ts := newTestServer(t, testServerOpts{autoApprove: &autoApprove})
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256"
	resp, err := httpClient.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	csrf := extractInputValue(string(body), "csrf_token")
	if csrf == "" {
		t.Fatal("missing csrf token")
	}
	values := url.Values{}
	values.Set("csrf_token", csrf)
	values.Set("approved", "yes")
	resp, err = httpClient.Post(authURL, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d body = %s", resp.StatusCode, readBody(resp.Body))
	}
	if !strings.Contains(resp.Header.Get("Location"), "code=") {
		t.Fatalf("location = %q", resp.Header.Get("Location"))
	}
}

func TestEHRLaunchFlow(t *testing.T) {
	_, srv, ts := newTestServer(t, testServerOpts{})
	defer ts.Close()

	values := url.Values{}
	values.Set("launch_issuer_id", "ehr-launcher")
	values.Set("launch_issuer_secret", "ehr-secret")
	values.Set("patient", "launch-patient-42")
	resp, err := http.Post(ts.URL+"/oauth/launch", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var launchDoc map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&launchDoc); err != nil {
		t.Fatal(err)
	}
	launchToken := launchDoc["launch"]
	if launchToken == "" {
		t.Fatalf("launchDoc = %#v", launchDoc)
	}

	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authURL := ts.URL + "/oauth/authorize?response_type=code&client_id=standalone-app&redirect_uri=https://app.example/callback&scope=patient/Patient.read%20launch/patient&launch=" + url.QueryEscape(launchToken) + "&aud=" + url.QueryEscape(srv.FHIRBaseURL()) + "&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256"
	resp, err = noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	smartClient, err := client.New(client.Config{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	tokenResp := mustExchange(t, smartClient, srv.TokenEndpoint(), u.Query().Get("code"), pkce)
	if tokenResp.Patient != "launch-patient-42" {
		t.Fatalf("patient = %q", tokenResp.Patient)
	}
}

func extractInputValue(htmlBody, name string) string {
	needle := `name="` + name + `" value="`
	idx := strings.Index(htmlBody, needle)
	if idx < 0 {
		return ""
	}
	rest := htmlBody[idx+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func readBody(r io.Reader) string {
	body, _ := io.ReadAll(r)
	return string(body)
}
