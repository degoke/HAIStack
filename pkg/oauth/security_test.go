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
