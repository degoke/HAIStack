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

	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		ClientID: "sqlite-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.ConsumeAuthorizationCode("code-1")
	if !ok || entry.ClientID != "sqlite-client" {
		t.Fatalf("code = %+v ok=%v", entry, ok)
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
