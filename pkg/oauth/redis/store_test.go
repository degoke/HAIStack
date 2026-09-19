package redis_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthredis "github.com/degoke/health-ai-stack/pkg/oauth/redis"
	goredis "github.com/redis/go-redis/v9"
)

func TestEphemeralStores_RoundTrip(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, replayStore, revocationStore := oauthredis.EphemeralStores(client, "test:")
	now := time.Now()

	const issuer = "https://auth.example.test"
	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "redis-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.ConsumeAuthorizationCode(issuer, "code-1")
	if !ok || entry.ClientID != "redis-client" {
		t.Fatalf("code = %+v ok=%v", entry, ok)
	}
	if err := authStore.SaveAuthorizationCode("code-2", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "redis-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode("https://other.example", "code-2"); ok {
		t.Fatal("cross-issuer consume must fail")
	}
	entry, ok = authStore.ConsumeAuthorizationCode(issuer, "code-2")
	if !ok || entry.ClientID != "redis-client" {
		t.Fatalf("code-2 = %+v ok=%v", entry, ok)
	}

	refresh := "refresh-1"
	if err := authStore.SaveRefreshToken(refresh, oauth.RefreshTokenEntry{
		Issuer: issuer, ClientID: "owner", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if authStore.DeleteRefreshTokenForClient(issuer, refresh, "other") {
		t.Fatal("expected foreign client revoke to fail")
	}
	if !authStore.DeleteRefreshTokenForClient(issuer, refresh, "owner") {
		t.Fatal("expected owner revoke to succeed")
	}
	if authStore.DeleteRefreshTokenForClient(issuer, refresh, "owner") {
		t.Fatal("expected second revoke to fail after delete")
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

func TestNewServer_RequiresClientRegistry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	_, err = oauthredis.NewServer(oauth.Config{Issuer: "https://auth.example"}, client, "test:")
	if err == nil {
		t.Fatal("expected error without client registry")
	}
}
