package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestSQLiteStores_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	authStore, clientStore, replayStore, revocationStore := oauthstore.SQLiteStores(db.SQL())
	now := time.Now()
	if err := clientStore.Register(oauth.Client{
		ClientID:     "sqlite-client",
		ClientSecret: "sqlite-secret",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	}); err != nil {
		t.Fatal(err)
	}
	client, ok := clientStore.Get("sqlite-client")
	if !ok || client.ClientID != "sqlite-client" || client.ClientSecretHash == "" {
		t.Fatalf("client = %+v ok=%v", client, ok)
	}

	const issuer = "https://auth.example.test"
	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "sqlite-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode("https://other.example", "code-1"); ok {
		t.Fatal("expected cross-issuer consume to fail")
	}
	entry, ok := authStore.ConsumeAuthorizationCode(issuer, "code-1")
	if !ok || entry.ClientID != "sqlite-client" {
		t.Fatalf("code = %+v ok=%v", entry, ok)
	}

	const issuerB = "https://auth.example/t/clinic-b"
	if err := authStore.SaveAuthorizationCode("shared-code", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "sqlite-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := authStore.SaveAuthorizationCode("shared-code", oauth.AuthorizationCode{
		Issuer: issuerB, ClientID: "sqlite-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuer, "shared-code"); !ok {
		t.Fatal("expected code for issuer A")
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuerB, "shared-code"); !ok {
		t.Fatal("expected code for issuer B")
	}

	if err := replayStore.CheckAndStore("jti-1", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := replayStore.CheckAndStore("jti-1", now.Add(5*time.Minute)); err == nil {
		t.Fatal("expected replay rejection")
	}

	if err := revocationStore.Revoke("access-jti-1", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !revocationStore.IsRevoked("access-jti-1") {
		t.Fatal("expected revoked jti")
	}
}

func TestSQLiteStores_FractionalExpiryIsNotLexicographic(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-expiry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	authStore, _, _, _ := oauthstore.SQLiteStores(db.SQL())
	sqlStore := authStore.(*oauthstore.SQLiteAuthorizationStore)
	now := time.Date(2026, 1, 1, 12, 0, 0, 500000000, time.UTC)
	sqlStore.Now = func() time.Time { return now }

	const issuer = "https://auth.example.test"
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO hai_oauth_auth_code (code, issuer, payload, expires_at)
		VALUES (?, ?, ?, ?)`,
		"expired-code", issuer, `{"issuer":"https://auth.example.test","clientId":"c"}`, "2026-01-01T12:00:00Z",
	); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuer, "expired-code"); ok {
		t.Fatal("lexicographic RFC3339Nano compare would treat 12:00:00Z as still valid at 12:00:00.5Z")
	}

	if err := authStore.SaveRefreshToken("refresh-1", oauth.RefreshTokenEntry{
		Issuer: issuer, ClientID: "c", Scope: "patient/*.read", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeRefreshToken("https://other.example", "refresh-1"); ok {
		t.Fatal("expected cross-issuer refresh consume to fail")
	}
	entry, ok := authStore.ConsumeRefreshToken(issuer, "refresh-1")
	if !ok || entry.ClientID != "c" {
		t.Fatalf("refresh = %+v ok=%v", entry, ok)
	}
	if _, ok := authStore.ConsumeRefreshToken(issuer, "refresh-1"); ok {
		t.Fatal("expected refresh consume-once")
	}
}

func TestSQLiteClientRegistry_IsolatesIssuers(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-clients.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, clients, _, _ := oauthstore.SQLiteStores(db.SQL())
	scoped := clients.(oauth.IssuerScopedClientRegistry)
	a := scoped.ForIssuer("https://auth.example/t/a")
	b := scoped.ForIssuer("https://auth.example/t/b")
	if err := a.Register(oauth.Client{ClientID: "shared-id", ClientSecret: "a-secret"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get("shared-id"); ok {
		t.Fatal("expected tenant B not to see tenant A client")
	}
	if _, ok := a.Get("shared-id"); !ok {
		t.Fatal("expected tenant A client")
	}
}
