package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestOAuthServer_IntrospectAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	secret := "introspect-secret"
	_ = server.RegisterClient(oauth.Client{
		ClientID:                "intro-client",
		ClientSecret:            secret,
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "intro-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, _ := noRedirect.Get(authURL)
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "intro-client", ClientSecret: secret,
		RedirectURI: "https://localhost/callback", Code: code, PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("token", tokenResp.AccessToken)
	form.Set("client_id", "intro-client")
	form.Set("client_secret", secret)
	resp, err := http.PostForm(base+"/oauth/introspect", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["active"] != true {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestOAuthServer_IntrospectRequiresConfidentialClient(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/", server.Handler())

	form := url.Values{}
	form.Set("token", "opaque")
	resp, err := http.PostForm(base+"/oauth/introspect", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestOAuthServer_VerificationKeysValidatePreviousKid(t *testing.T) {
	oldKey, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	base := strings.TrimSuffix(ts.URL, "/")

	issuer, err := oauth.NewServer(oauth.Config{
		Issuer: base, FHIRAudience: base, AutoApprove: true, SigningKey: oldKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = issuer.RegisterClient(oauth.Client{
		ClientID:     "rotate-client",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	mux.Handle("/", issuer.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: &client.SMARTConfiguration{
			AuthorizationEndpoint: base + "/oauth/authorize",
			TokenEndpoint:         base + "/oauth/token",
		},
		ClientID: "rotate-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := noRedirect.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: base + "/oauth/token", ClientID: "rotate-client",
		RedirectURI: "https://localhost/callback", Code: code, PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := oauth.NewServer(oauth.Config{
		Issuer:           base,
		FHIRAudience:     base,
		SigningKey:       newKey,
		VerificationKeys: []*oauth.KeySet{oldKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	intro := rotated.IntrospectToken(tokenResp.AccessToken, "access_token")
	if !intro.Active {
		t.Fatalf("expected rotated verifier to accept previous kid, got %#v", intro)
	}
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{DefaultTenantID: "local", DefaultUserRoles: []string{"clinician"}})
	bearer := rotated.BearerAuthConfig(adapter)
	if _, err := bearer.Validator.ValidateToken(tokenResp.AccessToken, bearer.Options); err != nil {
		t.Fatalf("bearer validate previous kid: %v", err)
	}

	jwksRec := httptest.NewRecorder()
	rotated.Handler().ServeHTTP(jwksRec, httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil))
	body := jwksRec.Body.String()
	if !strings.Contains(body, oldKey.KeyID) || !strings.Contains(body, newKey.KeyID) {
		t.Fatalf("jwks missing rotated keys: %s", body)
	}
}
