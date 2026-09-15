package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestNewProductionServer_FileBackedStores(t *testing.T) {
	dir := t.TempDir()
	paths := oauth.DefaultProductionPaths(dir)
	server, err := oauth.NewProductionServer(oauth.Config{
		Issuer:       "https://issuer.example",
		FHIRAudience: "https://issuer.example",
		AutoApprove:  true,
	}, paths)
	if err != nil {
		t.Fatal(err)
	}
	_ = server.RegisterClient(oauth.Client{
		ClientID:                "prod-client",
		ClientSecret:            "prod-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	})
	mux := http.NewServeMux()
	mux.Handle("/", server.Handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")

	restarted, err := oauth.NewProductionServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		AutoApprove:  true,
	}, oauth.ProductionPaths{
		StateDir:   dir,
		Clients:    paths.Clients,
		Tokens:     paths.Tokens,
		Replay:     paths.Replay,
		SigningKey: paths.SigningKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux2 := http.NewServeMux()
	mux2.Handle("/", restarted.Handler())
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()
	base2 := strings.TrimSuffix(srv2.URL, "/")

	httpClient, _ := client.New(client.Config{BaseURL: base2})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base2)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "prod-client", RedirectURI: "https://localhost/callback",
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
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "prod-client",
		ClientSecret:  "prod-secret",
		RedirectURI:   "https://localhost/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}
	if _, err := filepath.Glob(filepath.Join(dir, "*")); err != nil {
		t.Fatal(err)
	}
}
