package store_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/haistack/pkg/client"
	"github.com/degoke/haistack/pkg/oauth"
	oauthstore "github.com/degoke/haistack/pkg/oauth/store"
	"github.com/degoke/haistack/pkg/sqlite"
)

type swappingHandler struct {
	mu      sync.Mutex
	current http.Handler
}

func (h *swappingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cur := h.current
	h.mu.Unlock()
	if cur == nil {
		http.NotFound(w, r)
		return
	}
	cur.ServeHTTP(w, r)
}

func (h *swappingHandler) set(next http.Handler) {
	h.mu.Lock()
	h.current = next
	h.mu.Unlock()
}

func TestSQLiteServer_PersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "oauth.db")
	keyPaths := oauth.DefaultSigningKeyPaths(filepath.Join(dir, "keys"))
	created, err := oauth.LoadOrCreateSigningKey(keyPaths.SigningKey, keyPaths.SigningKID)
	if err != nil {
		t.Fatal(err)
	}

	handler := &swappingHandler{}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	base := strings.TrimSuffix(ts.URL, "/")

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	server, err := oauthstore.NewSQLiteServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		SigningKey:   created,
		AutoApprove:  true,
	}, db.SQL())
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := server.RegisterClient(oauth.Client{
		ClientID:                "persist-client",
		ClientSecret:            "persist-secret",
		TokenEndpointAuthMethod: oauth.AuthMethodClientSecretPost,
		RedirectURIs:            []string{"https://localhost/callback"},
		Scopes:                  []string{"patient/Patient.rs"},
	}); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	handler.set(server.Handler())

	httpClient, _ := client.New(client.Config{BaseURL: base})
	cfg, _ := httpClient.SMART().Discover(context.Background(), base)
	pkce, _ := client.NewPKCEChallenge()
	authURL, _ := httpClient.SMART().BuildAuthURL(client.AuthCodeRequest{
		Config: cfg, ClientID: "persist-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", PKCE: pkce,
	})
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	authResp, err := noRedirect.Get(authURL)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusFound {
		_ = db.Close()
		t.Fatalf("authorize status = %d", authResp.StatusCode)
	}
	code := strings.Split(strings.Split(authResp.Header.Get("Location"), "code=")[1], "&")[0]
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := oauth.LoadSigningKey(keyPaths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.KeyID != created.KeyID {
		t.Fatalf("pem reload kid = %v want %q", loaded, created.KeyID)
	}

	reopened, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(ctx); err != nil {
		_ = reopened.Close()
		t.Fatal(err)
	}
	restarted, err := oauthstore.NewSQLiteServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		SigningKey:   loaded,
		AutoApprove:  true,
	}, reopened.SQL())
	if err != nil {
		t.Fatal(err)
	}
	handler.set(restarted.Handler())

	tokenResp, err := httpClient.SMART().ExchangeAuthCode(context.Background(), client.AuthCodeExchangeRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "persist-client",
		ClientSecret:  "persist-secret",
		RedirectURI:   "https://localhost/callback",
		Code:          code,
		PKCE:          pkce,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
		t.Fatalf("token resp missing access or refresh: %+v", tokenResp)
	}

	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	reloadedKey, err := oauth.LoadSigningKey(keyPaths)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedKey == nil || reloadedKey.KeyID != created.KeyID {
		t.Fatalf("second pem reload kid = %v want %q", reloadedKey, created.KeyID)
	}
	again, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	if err := again.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	third, err := oauthstore.NewSQLiteServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		SigningKey:   reloadedKey,
		AutoApprove:  true,
	}, again.SQL())
	if err != nil {
		t.Fatal(err)
	}
	handler.set(third.Handler())

	refreshed, err := httpClient.SMART().RefreshToken(context.Background(), client.RefreshTokenRequest{
		TokenEndpoint: cfg.TokenEndpoint,
		ClientID:      "persist-client",
		ClientSecret:  "persist-secret",
		RefreshToken:  tokenResp.RefreshToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("missing access token after refresh")
	}
}
