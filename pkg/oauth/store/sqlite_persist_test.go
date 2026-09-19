package store_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/client"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestSQLiteServer_PersistsClientsAndCodesAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	key, err := oauth.NewKeySet(2048)
	if err != nil {
		t.Fatal(err)
	}

	var current http.Handler
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		h := current
		mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	}))
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
		SigningKey:   key,
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
	mu.Lock()
	current = server.Handler()
	mu.Unlock()

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

	reopened, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := oauthstore.NewSQLiteServer(oauth.Config{
		Issuer:       base,
		FHIRAudience: base,
		SigningKey:   key,
		AutoApprove:  true,
	}, reopened.SQL())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	current = restarted.Handler()
	mu.Unlock()

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
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}
}
