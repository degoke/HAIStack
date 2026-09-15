package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

func TestOAuthServer_PrivateKeyJWTAuthCodeExchange(t *testing.T) {
	key, pemPub := mustRSAKey(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	now := time.Now()
	server, err := oauth.NewServer(oauth.Config{
		Issuer: base, FHIRAudience: base, AutoApprove: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:                "jwt-client",
		PublicKeyPEM:            pemPub,
		Algorithm:               "RS256",
		TokenEndpointAuthMethod: oauth.AuthMethodPrivateKeyJWT,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "jwt-client", RedirectURI: "https://localhost/callback",
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

	assertion := signedAssertionJWT(t, key, map[string]any{
		"iss": "jwt-client", "sub": "jwt-client", "aud": base + "/oauth/token",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": "jwt-auth-code-1",
	})
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://localhost/callback")
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	form.Set("code_verifier", pkce.Verifier)
	resp, err := http.PostForm(cfg.TokenEndpoint, form)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status=%d", resp.StatusCode)
	}
}

func TestOAuthServer_AccessTokenRevocationDenylist(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:     "revoke-access-client",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "revoke-access-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, _ := noRedirect.Get(authURL)
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "revoke-access-client",
		RedirectURI: "https://localhost/callback", Code: code, PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{DefaultTenantID: "t1"})
	bearer := server.BearerAuthConfig(adapter)
	resolver := hahttp.SMARTBearerPrincipalResolver(bearer)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	if _, _, err := resolver(context.Background(), req); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	revokeForm := url.Values{}
	revokeForm.Set("token", tokenResp.AccessToken)
	revokeForm.Set("token_type_hint", "access_token")
	revokeForm.Set("client_id", "revoke-access-client")
	revokeResp, err := http.PostForm(base+"/oauth/revoke", revokeForm)
	if err != nil {
		t.Fatal(err)
	}
	_ = revokeResp.Body.Close()

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	if _, _, err := resolver(context.Background(), req2); err == nil {
		t.Fatal("expected revoked access token to be rejected")
	}
}
