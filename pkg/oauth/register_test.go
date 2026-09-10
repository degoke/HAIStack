package oauth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestDynamicClientRegistration(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cfg := oauth.Config{
		Issuer:      "http://example.test",
		FHIRBaseURL: "http://example.test/fhir",
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:     oauth.NewClientRegistry(),
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

	body := `{"client_name":"Dynamic App","redirect_uris":["https://app.example/callback"],"token_endpoint_auth_method":"none"}`
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(body))
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
	clientID, _ := doc["client_id"].(string)
	if clientID == "" {
		t.Fatalf("doc = %#v", doc)
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("redirect_uri", "https://app.example/callback")
	values.Set("scope", "openid")
	values.Set("code_challenge", "abc")
	values.Set("code_challenge_method", "S256")
	autoApprove := true
	cfg2 := cfg
	cfg2.AutoApprove = &autoApprove
	srv2, err := oauth.NewServer(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = noRedirect.Get(ts2.URL + "/oauth/authorize?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d", resp.StatusCode)
	}
}

func TestSQLiteConsentSessionStore(t *testing.T) {
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
	params := url.Values{}
	params.Set("client_id", "app")
	params.Set("redirect_uri", "https://app/cb")
	sessionID, csrf, err := stores.Consent.Create("https://example.com", params, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	restored, subject, err := stores.Consent.Consume("https://example.com", sessionID, csrf)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "user-1" || restored.Get("client_id") != "app" {
		t.Fatalf("restored = %#v subject = %q", restored, subject)
	}
	_, _, err = stores.Consent.Consume("https://example.com", sessionID, csrf)
	if err == nil {
		t.Fatal("expected single-use session failure")
	}
}

func TestRegistrationEndpointAdvertised(t *testing.T) {
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
	doc := srv.SMARTConfiguration()
	if doc.RegistrationEndpoint == "" {
		t.Fatal("missing registration_endpoint")
	}
}

func TestDynamicRegistrationDisabled(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:                    "http://example.test",
		FHIRBaseURL:               "http://example.test/fhir",
		Signer:                    oauth.RS256Signer{PrivateKey: key, Kid: "test"},
		Clients:                   oauth.NewClientRegistry(),
		DynamicClientRegistration: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", bytes.NewReader([]byte(`{"redirect_uris":["https://app/cb"]}`)))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
