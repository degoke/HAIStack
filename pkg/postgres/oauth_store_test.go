package postgres_test

import (
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthpostgres "github.com/degoke/health-ai-stack/pkg/oauth/postgres"
)

func TestOAuthPostgresStores_RoundTrip(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	authStore, clientStore, replayStore, revocationStore := oauthpostgres.Stores(db.Pool())
	now := time.Now()
	if err := clientStore.Register(oauth.Client{
		ClientID:     "pg-client",
		ClientSecret: "pg-secret",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	}); err != nil {
		t.Fatal(err)
	}
	client, ok := clientStore.Get("pg-client")
	if !ok || client.ClientID != "pg-client" || client.ClientSecretHash == "" {
		t.Fatalf("client = %+v ok=%v", client, ok)
	}

	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		ClientID: "pg-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.ConsumeAuthorizationCode("code-1")
	if !ok || entry.ClientID != "pg-client" {
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
