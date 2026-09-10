package redis_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthredis "github.com/degoke/health-ai-stack/pkg/oauth/redis"
	goredis "github.com/redis/go-redis/v9"
)

func TestStores_RoundTrip(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, clientStore, replayStore, revocationStore := oauthredis.Stores(client, "test:")
	now := time.Now()

	if err := clientStore.Register(oauth.Client{
		ClientID:     "redis-client",
		ClientSecret: "redis-secret",
		RedirectURIs: []string{"https://localhost/callback"},
		Scopes:       []string{"patient/Patient.rs"},
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := clientStore.Get("redis-client")
	if !ok || got.ClientID != "redis-client" || got.ClientSecretHash == "" {
		t.Fatalf("client = %+v ok=%v", got, ok)
	}

	if err := authStore.SaveAuthorizationCode("code-1", oauth.AuthorizationCode{
		ClientID: "redis-client", RedirectURI: "https://localhost/callback",
		Scope: "patient/Patient.rs", ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.ConsumeAuthorizationCode("code-1")
	if !ok || entry.ClientID != "redis-client" {
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
