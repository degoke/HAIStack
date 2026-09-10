package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSMARTDiscoveryParsing(t *testing.T) {
	var gotUserAgent, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/smart-configuration" {
			http.NotFound(w, r)
			return
		}
		gotUserAgent = r.Header.Get("User-Agent")
		gotHeader = r.Header.Get("X-Test-Header")
		_, _ = w.Write([]byte(`{
			"issuer":"https://fhir.example.com",
			"authorization_endpoint":"https://fhir.example.com/auth",
			"token_endpoint":"https://fhir.example.com/token",
			"code_challenge_methods_supported":["S256"]
		}`))
	}))
	defer srv.Close()

	c, _ := New(Config{
		BaseURL: srv.URL,
		DefaultHeaders: map[string]string{
			"X-Test-Header": "smart-discovery",
		},
		UserAgent: "haistack-client-test",
	})
	cfg, err := c.SMART().Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if cfg.AuthorizationEndpoint == "" || cfg.TokenEndpoint == "" {
		t.Fatalf("config: %+v", cfg)
	}
	if len(cfg.CodeChallengeMethods) != 1 || cfg.CodeChallengeMethods[0] != "S256" {
		t.Fatalf("pkce methods: %v", cfg.CodeChallengeMethods)
	}
	if gotUserAgent != "haistack-client-test" || gotHeader != "smart-discovery" {
		t.Fatalf("headers: user-agent=%q x-test-header=%q", gotUserAgent, gotHeader)
	}
}

func TestPKCEChallenge(t *testing.T) {
	pkce, err := NewPKCEChallenge()
	if err != nil {
		t.Fatalf("NewPKCEChallenge: %v", err)
	}
	if pkce.Verifier == "" || pkce.Challenge == "" || pkce.Method != "S256" {
		t.Fatalf("pkce: %+v", pkce)
	}
	if pkce.Challenge == pkce.Verifier {
		t.Fatal("challenge should differ from verifier")
	}
}

func TestBuildAuthURL(t *testing.T) {
	c, _ := New(Config{BaseURL: "http://example.com"})
	pkce, _ := NewPKCEChallenge()
	u, err := c.SMART().BuildAuthURL(AuthCodeRequest{
		Config:      &SMARTConfiguration{AuthorizationEndpoint: "https://auth.example/authorize"},
		ClientID:    "client-1",
		RedirectURI: "https://app.example/callback",
		Scope:       "launch/patient patient/*.read",
		State:       "state-1",
		PKCE:        pkce,
	})
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != "client-1" {
		t.Fatalf("query: %v", q)
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("pkce method: %s", q.Get("code_challenge_method"))
	}
}

func TestAuthCodeTokenExchange(t *testing.T) {
	var gotGrant, gotCode, gotVerifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		gotCode = r.Form.Get("code")
		gotVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","token_type":"Bearer","expires_in":3600,"scope":"patient/*.read"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	pkce, _ := NewPKCEChallenge()
	resp, err := c.SMART().ExchangeAuthCode(context.Background(), AuthCodeExchangeRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "client-1",
		RedirectURI:   "https://app/cb",
		Code:          "code-abc",
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if gotGrant != "authorization_code" || gotCode != "code-abc" || gotVerifier != pkce.Verifier {
		t.Fatalf("form: grant=%s code=%s verifier=%s", gotGrant, gotCode, gotVerifier)
	}
	if resp.AccessToken != "tok-1" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestAuthCodeTokenExchangeWithClientSecret(t *testing.T) {
	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotSecret = r.Form.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-secret","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	resp, err := c.SMART().ExchangeAuthCode(context.Background(), AuthCodeExchangeRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "client-1",
		ClientSecret:  "secret-value",
		RedirectURI:   "https://app/cb",
		Code:          "code-abc",
	})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if gotSecret != "secret-value" {
		t.Fatalf("client_secret = %q", gotSecret)
	}
	if resp.AccessToken != "tok-secret" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestAuthCodeTokenExchangeWithClientSecretBasic(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-basic","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	resp, err := c.SMART().ExchangeAuthCode(context.Background(), AuthCodeExchangeRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "client-1",
		ClientSecret:  "secret-value",
		ClientAuth:    ClientAuthSecretBasic,
		RedirectURI:   "https://app/cb",
		Code:          "code-abc",
	})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if gotUser != "client-1" || gotPass != "secret-value" {
		t.Fatalf("basic auth = %q:%q", gotUser, gotPass)
	}
	if resp.AccessToken != "tok-basic" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestRefreshTokenWithClientSecret(t *testing.T) {
	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotSecret = r.Form.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-refresh","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	resp, err := c.SMART().RefreshToken(context.Background(), RefreshTokenRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "client-1",
		ClientSecret:  "refresh-secret",
		RefreshToken:  "refresh-abc",
	})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if gotSecret != "refresh-secret" {
		t.Fatalf("client_secret = %q", gotSecret)
	}
	if resp.AccessToken != "tok-refresh" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestClientAssertionGeneration(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	var gotAssertion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotAssertion = r.Form.Get("client_assertion")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"backend-tok","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	resp, err := c.SMART().ExchangeClientAssertion(context.Background(), ClientAssertionRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "backend-client",
		Scope:         "system/*.read",
		PrivateKey:    key,
	})
	if err != nil {
		t.Fatalf("ExchangeClientAssertion: %v", err)
	}
	if gotAssertion == "" || strings.Count(gotAssertion, ".") != 2 {
		t.Fatalf("assertion format: %q", gotAssertion)
	}
	if resp.AccessToken != "backend-tok" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestAuthCodeExchangeWithPrivateKeyJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	var gotAssertion, gotGrant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		gotAssertion = r.Form.Get("client_assertion")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"jwt-tok","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	pkce, _ := NewPKCEChallenge()
	resp, err := c.SMART().ExchangeAuthCode(context.Background(), AuthCodeExchangeRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "jwt-client",
		ClientAuth:    ClientAuthPrivateKeyJWT,
		ClientJWT:     &ClientJWTAuth{PrivateKey: key},
		RedirectURI:   "https://app/cb",
		Code:          "code-abc",
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if gotGrant != "authorization_code" || gotAssertion == "" {
		t.Fatalf("grant=%s assertion=%q", gotGrant, gotAssertion)
	}
	if resp.AccessToken != "jwt-tok" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestRefreshTokenWithPrivateKeyJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	var gotAssertion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotAssertion = r.Form.Get("client_assertion")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"jwt-refresh","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	resp, err := c.SMART().RefreshToken(context.Background(), RefreshTokenRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "jwt-client",
		ClientAuth:    ClientAuthPrivateKeyJWT,
		ClientJWT:     &ClientJWTAuth{PrivateKey: key},
		RefreshToken:  "refresh-abc",
	})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if gotAssertion == "" {
		t.Fatal("expected client_assertion")
	}
	if resp.AccessToken != "jwt-refresh" {
		t.Fatalf("token: %s", resp.AccessToken)
	}
}

func TestRevokeTokenWithPrivateKeyJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	var gotAssertion, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotAssertion = r.Form.Get("client_assertion")
		gotToken = r.Form.Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	err = c.SMART().RevokeToken(context.Background(), RevokeTokenRequest{
		RevocationEndpoint: srv.URL,
		ClientID:           "jwt-client",
		ClientAuth:         ClientAuthPrivateKeyJWT,
		ClientJWT:          &ClientJWTAuth{PrivateKey: key},
		Token:              "access-tok",
		TokenTypeHint:      "access_token",
	})
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if gotAssertion == "" || gotToken != "access-tok" {
		t.Fatalf("assertion=%q token=%q", gotAssertion, gotToken)
	}
}

func TestTokenResponseParsing(t *testing.T) {
	var tr TokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"a","token_type":"Bearer","patient":"p1"}`), &tr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tr.Patient != "p1" {
		t.Fatalf("patient: %s", tr.Patient)
	}
}
