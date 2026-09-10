package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestOAuthServer_RejectsEmptyRedirectURIList(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{ClientID: "bad-client", RedirectURIs: nil})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "bad-client", RedirectURI: "https://app.example/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	resp, err := (&http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestFileAuthorizationStore_PersistsPendingSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-pending.json")
	store, err := oauth.NewFileAuthorizationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePendingAuthorization("sess-1", oauth.PendingAuthorization{
		Request:   oauth.AuthorizationRequest{ClientID: "client", Scope: "patient/*.rs"},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := oauth.NewFileAuthorizationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reloaded.ConsumePendingAuthorization("sess-1")
	if !ok || entry.Request.ClientID != "client" {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
}

func TestOAuthServer_ConfidentialClientSecretPost(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{Issuer: base, FHIRAudience: base, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{
		ClientID:     "conf-client",
		ClientSecret: "super-secret",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "conf-client", RedirectURI: "https://localhost/callback",
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
		ClientID:      "conf-client",
		ClientSecret:  "super-secret",
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
}

func TestOAuthServer_ConsentCSRFRequired(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.TrimSuffix(srv.URL, "/")
	server, err := oauth.NewServer(oauth.Config{
		Issuer: base, FHIRAudience: base, RequireConsentForm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterClient(oauth.Client{
		ClientID: "csrf-client", RedirectURIs: []string{"https://localhost/callback"},
		Scopes: []string{"patient/Patient.rs"},
	})
	mux.Handle("/", server.Handler())
	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "csrf-client", RedirectURI: "https://localhost/callback",
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
	consentURL := authResp.Header.Get("Location")
	badResp, err := noRedirect.Post(consentURL, "application/x-www-form-urlencoded", strings.NewReader("approve=yes"))
	if err != nil {
		t.Fatal(err)
	}
	_ = badResp.Body.Close()
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf rejection, status=%d", badResp.StatusCode)
	}
}

func TestFileAuthorizationStore_PersistsCodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-auth.json")
	store, err := oauth.NewFileAuthorizationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		ClientID: "client", RedirectURI: "https://app/cb", Scope: "patient/*.rs",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := oauth.NewFileAuthorizationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reloaded.ConsumeAuthorizationCode("code-1")
	if !ok || entry.ClientID != "client" {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
}
