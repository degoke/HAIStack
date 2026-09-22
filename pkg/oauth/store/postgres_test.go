package store_test

import (
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/oauth"
	oauthstore "github.com/degoke/haistack/pkg/oauth/store"
	"github.com/degoke/haistack/pkg/testkit/postgrestest"
)

func TestPostgresStores_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres store test in short mode")
	}
	db := postgrestest.SharedDB(t)

	authStore, clientStore, replayStore, revocationStore := oauthstore.PostgresStores(db.Pool())
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

	const issuer = "https://auth.example.test"
	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "pg-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.ConsumeAuthorizationCode(issuer, "code-1")
	if !ok || entry.ClientID != "pg-client" {
		t.Fatalf("code = %+v ok=%v", entry, ok)
	}

	const issuerB = "https://auth.example/t/clinic-b"
	if err := authStore.SaveAuthorizationCode("shared-code", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "pg-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := authStore.SaveAuthorizationCode("shared-code", oauth.AuthorizationCode{
		Issuer: issuerB, ClientID: "pg-client", RedirectURI: "https://localhost/callback",
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
