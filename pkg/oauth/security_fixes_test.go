package oauth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestDynamicRegistrationRejectsClientID(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     oauth.NewClientRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"client_id":"attacker-id","redirect_uris":["https://app.example/callback"],"token_endpoint_auth_method":"none"}`
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSeededClientSurvivesDynamicRegistration(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "seed-client",
			RedirectURIs: []string{"https://seed.example/callback"},
			Scopes:       []string{"openid"},
		},
	})
	cfg := oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
	}
	if err := oauthstore.ApplySQLiteStores(&cfg, db.SQL()); err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["https://evil.example/callback"],"token_endpoint_auth_method":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	client, err := srv.LookupClient("seed-client")
	if err != nil {
		t.Fatal(err)
	}
	if client.RedirectURIs[0] != "https://seed.example/callback" {
		t.Fatalf("seed client overwritten: %#v", client.RedirectURIs)
	}
}

func TestClientSecretBasicAuth(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	secret := "super-secret-value"
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:                "confidential-app",
			RedirectURIs:            []string{"https://app.example/callback"},
			Scopes:                  []string{"openid"},
			TokenEndpointAuthMethod: "client_secret_basic",
		},
		Confidential: true,
		ClientSecret: secret,
	})
	autoApprove := true
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     reg,
		AutoApprove: &autoApprove,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", "confidential-app")
	values.Set("redirect_uri", "https://app.example/callback")
	values.Set("scope", "openid")
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/oauth/authorize?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	loc := resp.Header.Get("Location")
	_ = resp.Body.Close()
	code := parseCodeFromRedirect(t, loc)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://app.example/callback")
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("confidential-app", secret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
}

func TestRegistrationRequiresAccessToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:                  "http://example.test",
		FHIRBaseURL:             "http://example.test/fhir",
		Signer:                  oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:                 oauth.NewClientRegistry(),
		RegistrationAccessToken: "registration-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"redirect_uris":["https://app.example/callback"],"token_endpoint_auth_method":"none"}`
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/oauth/register", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer registration-secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSQLiteClientSecretHashedAtRest(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	stores, err := oauthstore.NewSQLiteStores(db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	plainSecret := "stored-secret-value"
	client := oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "hashed-client",
			RedirectURIs: []string{"https://app.example/callback"},
			Scopes:       []string{"openid"},
		},
		Confidential: true,
		ClientSecret: plainSecret,
	}
	if err := stores.Clients.Upsert("http://example.test", client); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT client_secret FROM hai_oauth_client WHERE client_id = ? AND issuer = ?`,
		"hashed-client", "http://example.test").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == plainSecret {
		t.Fatal("expected hashed secret in database")
	}
	loaded, err := stores.Clients.Lookup("http://example.test", "hashed-client")
	if err != nil {
		t.Fatal(err)
	}
	if !oauth.VerifyClientSecret(loaded, plainSecret) {
		t.Fatal("expected loaded client to verify plaintext secret")
	}
}

func TestMultiTenantDynamicRegistrationUsesSharedStore(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tenantClients := oauth.NewClientRegistry()
	_ = tenantClients.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "tenant-static",
			RedirectURIs: []string{"https://tenant.example/callback"},
			Scopes:       []string{"openid"},
		},
	})
	base := oauth.Config{
		Signer:  oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients: oauth.NewClientRegistry(),
	}
	if err := oauthstore.ApplySQLiteStores(&base, db.SQL()); err != nil {
		t.Fatal(err)
	}
	tenants := oauth.NewTenantRegistry()
	_ = tenants.Register(oauth.TenantIssuerConfig{
		TenantID:    "tenant-a",
		Issuer:      "http://example.test/t/tenant-a",
		FHIRBaseURL: "http://example.test/t/tenant-a/fhir",
		Clients:     tenantClients,
	})
	mts, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{Base: base, Tenants: tenants})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mts.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/t/tenant-a/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["https://dynamic.example/callback"],"token_endpoint_auth_method":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	dynamicID, _ := doc["client_id"].(string)
	if dynamicID == "" {
		t.Fatal("missing dynamic client id")
	}

	srvA, err := mts.ServerForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srvA.LookupClient("tenant-static"); err != nil {
		t.Fatalf("static tenant client missing: %v", err)
	}
	if _, err := srvA.LookupClient(dynamicID); err != nil {
		t.Fatalf("dynamic tenant client missing: %v", err)
	}
}

func TestVerifyStoredClientSecretLegacyPlaintext(t *testing.T) {
	if !oauth.VerifyStoredClientSecret("legacy-secret", "legacy-secret") {
		t.Fatal("expected legacy plaintext secret to verify")
	}
	if oauth.VerifyStoredClientSecret("legacy-secret", "wrong-secret") {
		t.Fatal("expected mismatch to fail")
	}
}

func parseCodeFromRedirect(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("missing code in %q", loc)
	}
	return code
}
