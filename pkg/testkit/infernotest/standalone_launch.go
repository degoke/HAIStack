package infernotest

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
)

// AssertStandaloneLaunchFlow exercises Inferno-aligned standalone SMART launch:
// discovery at FHIR base, PKCE authorization code flow, and token exchange.
func AssertStandaloneLaunchFlow(t *testing.T, baseURL, fhirBase, clientID, redirectURI, scope string) {
	t.Helper()
	smartClient, err := client.New(client.Config{BaseURL: baseURL})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := smartClient.SMART().Discover(context.Background(), fhirBase)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthorizationEndpoint == "" || cfg.TokenEndpoint == "" {
		t.Fatalf("discovery = %#v", cfg)
	}
	pkce, err := client.NewPKCEChallenge()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := smartClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config:      cfg,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Scope:       scope,
		State:       "inferno-standalone",
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
		t.Fatalf("authorize status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "code=") {
		t.Fatalf("redirect = %q", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("state") != "inferno-standalone" {
		t.Fatalf("state = %q", u.Query().Get("state"))
	}
	tokenResp, err := smartClient.SMART().ExchangeAuthCode(context.Background(), cfg.TokenEndpoint, clientID, redirectURI, u.Query().Get("code"), pkce)
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}
}
