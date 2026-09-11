package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestDynamicRegistrationRejectsInsecureRedirectURI(t *testing.T) {
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

	body := `{"redirect_uris":["http://app.example/callback"],"token_endpoint_auth_method":"none"}`
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestOpenIDConfigurationIncludesRegistrationEndpoint(t *testing.T) {
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

	resp, err := http.Get(ts.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["registration_endpoint"] == "" {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestSQLiteConsentSessionPurgeExpired(t *testing.T) {
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
	consent, ok := stores.Consent.(*oauthstore.SQLiteConsentSessionStore)
	if !ok {
		t.Fatal("expected sqlite consent store")
	}
	expired := time.Now().Add(-time.Minute)
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO hai_oauth_consent_session (session_id, issuer, params, csrf, subject, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"expired-session", "https://example.com", "{}", "csrf", "", expired.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatal(err)
	}
	consent.Now = func() time.Time { return time.Now() }
	removed, err := stores.Consent.PurgeExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM hai_oauth_consent_session WHERE session_id = ?`, "expired-session").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected expired consent session to be purged, count = %d", count)
	}
}

func TestLaunchIssuerRegistryStoresHashedSecrets(t *testing.T) {
	reg := oauth.NewLaunchIssuerRegistry()
	if err := reg.Register("ehr-launcher", "launch-secret"); err != nil {
		t.Fatal(err)
	}
	if !reg.Validate("ehr-launcher", "launch-secret") {
		t.Fatal("expected launch secret to validate")
	}
	if reg.Validate("ehr-launcher", "wrong-secret") {
		t.Fatal("expected plaintext mismatch to fail")
	}
}
