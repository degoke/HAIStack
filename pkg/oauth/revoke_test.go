package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestOAuthServer_RevokeRefreshToken(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:                "revoke-client",
		ClientSecret:            "revoke-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs openid"},
	})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "revoke-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs openid", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, _ := noRedirect.Get(authURL)
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "revoke-client", ClientSecret: "revoke-secret",
		RedirectURI: "https://localhost/callback", Code: code, PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.IDToken == "" {
		t.Fatal("expected id_token for openid scope")
	}
	form := url.Values{}
	form.Set("token", tokenResp.RefreshToken)
	form.Set("client_id", "revoke-client")
	form.Set("client_secret", "revoke-secret")
	revokeResp, err := http.PostForm(base+"/oauth/revoke", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status=%d", revokeResp.StatusCode)
	}
	refreshed, err := httpClient.SMART().RefreshToken(context.Background(), client.RefreshTokenRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "revoke-client", ClientSecret: "revoke-secret",
		RefreshToken: tokenResp.RefreshToken,
	})
	if err == nil && refreshed.AccessToken != "" {
		t.Fatal("expected revoked refresh token to fail")
	}
}

func TestOAuthServer_RevokeRejectsForeignClient(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID: "owner-client", ClientSecret: "owner-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	})
	_ = server.RegisterClient(oauth.Client{
		ClientID: "other-client", ClientSecret: "other-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "owner-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, _ := noRedirect.Get(authURL)
	_ = authResp.Body.Close()
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "owner-client", ClientSecret: "owner-secret",
		RedirectURI: "https://localhost/callback", Code: code, PKCE: pkce,
	})
	if err != nil {
		t.Fatal(err)
	}

	foreignForm := url.Values{}
	foreignForm.Set("token", tokenResp.RefreshToken)
	foreignForm.Set("client_id", "other-client")
	foreignForm.Set("client_secret", "other-secret")
	foreignResp, err := http.PostForm(base+"/oauth/revoke", foreignForm)
	if err != nil {
		t.Fatal(err)
	}
	_ = foreignResp.Body.Close()

	refreshed, err := httpClient.SMART().RefreshToken(context.Background(), client.RefreshTokenRequest{
		TokenEndpoint: cfg.TokenEndpoint, ClientID: "owner-client", ClientSecret: "owner-secret",
		RefreshToken: tokenResp.RefreshToken,
	})
	if err != nil || refreshed.AccessToken == "" {
		t.Fatalf("foreign revoke should not invalidate token: err=%v", err)
	}
}
