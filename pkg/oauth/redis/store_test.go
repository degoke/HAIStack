package redis_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/degoke/haistack/pkg/oauth"
	oauthredis "github.com/degoke/haistack/pkg/oauth/redis"
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

func TestAuthorizationStore_MismatchedJSONIssuerDoesNotBurnCode(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, _, _ := oauthredis.EphemeralStores(client, "test:")
	const issuer = "https://auth.example.test"
	seg := base64.RawURLEncoding.EncodeToString([]byte(issuer))
	key := "test:authcode:" + seg + ":tampered"
	payload, err := json.Marshal(oauth.AuthorizationCode{
		Issuer:    "https://other.example",
		ClientID:  "redis-client",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(context.Background(), key, payload, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuer, "tampered"); ok {
		t.Fatal("mismatched JSON issuer must not consume")
	}
	if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 1 {
		t.Fatalf("expected key retained n=%d err=%v", n, err)
	}
}

func TestAuthorizationStore_ExpiredJSONDoesNotBurnCode(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, _, _ := oauthredis.EphemeralStores(client, "test:")
	const issuer = "https://auth.example.test"
	if err := authStore.SaveAuthorizationCode("stale", oauth.AuthorizationCode{
		Issuer: issuer, ClientID: "redis-client", RedirectURI: "https://localhost/callback",
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuer, "stale"); ok {
		t.Fatal("expired code must not consume")
	}
	seg := base64.RawURLEncoding.EncodeToString([]byte(issuer))
	key := "test:authcode:" + seg + ":stale"
	if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 1 {
		t.Fatalf("expected expired key retained n=%d err=%v", n, err)
	}
}

func TestAuthorizationStore_LuaExpiryWinsOverExpiresAt(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, _, _ := oauthredis.EphemeralStores(client, "test:")
	const issuer = "https://auth.example.test"
	seg := base64.RawURLEncoding.EncodeToString([]byte(issuer))
	past := time.Now().Add(-time.Minute)
	liveExp := time.Now().Add(time.Minute).UnixMilli()
	raw, err := json.Marshal(map[string]any{
		"issuer":    issuer,
		"clientId":  "redis-client",
		"expiresAt": past,
		"exp":       liveExp,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("authcode", func(t *testing.T) {
		key := "test:authcode:" + seg + ":skew"
		if err := client.Set(context.Background(), key, raw, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		entry, ok := authStore.ConsumeAuthorizationCode(issuer, "skew")
		if !ok || entry.ClientID != "redis-client" {
			t.Fatalf("Lua-unexpired payload must consume even if expiresAt is past: %+v ok=%v", entry, ok)
		}
		if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 0 {
			t.Fatalf("expected key deleted after consume n=%d err=%v", n, err)
		}
	})

	t.Run("refresh", func(t *testing.T) {
		key := "test:refresh:" + seg + ":skew"
		if err := client.HSet(context.Background(), key, "clientId", "redis-client", "payload", raw).Err(); err != nil {
			t.Fatal(err)
		}
		if err := client.Expire(context.Background(), key, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		entry, ok := authStore.ConsumeRefreshToken(issuer, "skew")
		if !ok || entry.ClientID != "redis-client" {
			t.Fatalf("Lua-unexpired refresh must consume even if expiresAt is past: %+v ok=%v", entry, ok)
		}
		if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 0 {
			t.Fatalf("expected refresh key deleted after consume n=%d err=%v", n, err)
		}
	})

	t.Run("pending", func(t *testing.T) {
		key := "test:pending:" + seg + ":skew"
		if err := client.Set(context.Background(), key, raw, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		entry, ok := authStore.ConsumePendingAuthorization(issuer, "skew")
		if !ok || entry.Issuer != issuer {
			t.Fatalf("Lua-unexpired pending must consume even if expiresAt is past: %+v ok=%v", entry, ok)
		}
		if n, err := client.Exists(context.Background(), key).Result(); err != nil || n != 0 {
			t.Fatalf("expected pending key deleted after consume n=%d err=%v", n, err)
		}
	})
}

func TestAuthorizationStore_LookupAndGetUseLuaExp(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, _, _ := oauthredis.EphemeralStores(client, "test:")
	const issuer = "https://auth.example.test"
	seg := base64.RawURLEncoding.EncodeToString([]byte(issuer))
	ctx := context.Background()

	live, err := json.Marshal(map[string]any{
		"issuer":    issuer,
		"clientId":  "redis-client",
		"expiresAt": time.Now().Add(-time.Minute),
		"exp":       time.Now().Add(time.Minute).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := json.Marshal(map[string]any{
		"issuer":    issuer,
		"clientId":  "redis-client",
		"expiresAt": time.Now().Add(time.Minute),
		"exp":       time.Now().Add(-time.Minute).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}

	refreshLive := "test:refresh:" + seg + ":live"
	if err := client.HSet(ctx, refreshLive, "clientId", "redis-client", "payload", live).Err(); err != nil {
		t.Fatal(err)
	}
	entry, ok := authStore.LookupRefreshToken(issuer, "live")
	if !ok || entry.ClientID != "redis-client" {
		t.Fatalf("lookup must follow exp, not expiresAt: %+v ok=%v", entry, ok)
	}
	if n, err := client.Exists(ctx, refreshLive).Result(); err != nil || n != 1 {
		t.Fatalf("lookup must not delete n=%d err=%v", n, err)
	}

	refreshStale := "test:refresh:" + seg + ":stale"
	if err := client.HSet(ctx, refreshStale, "clientId", "redis-client", "payload", stale).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.LookupRefreshToken(issuer, "stale"); ok {
		t.Fatal("lookup must reject past exp even if expiresAt is future")
	}
	if n, err := client.Exists(ctx, refreshStale).Result(); err != nil || n != 1 {
		t.Fatalf("expired lookup must not delete n=%d err=%v", n, err)
	}

	pendingLive := "test:pending:" + seg + ":live"
	if err := client.Set(ctx, pendingLive, live, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.GetPendingAuthorization(issuer, "live"); !ok {
		t.Fatal("get must follow exp, not expiresAt")
	}
	if n, err := client.Exists(ctx, pendingLive).Result(); err != nil || n != 1 {
		t.Fatalf("get must not delete n=%d err=%v", n, err)
	}

	pendingStale := "test:pending:" + seg + ":stale"
	if err := client.Set(ctx, pendingStale, stale, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.GetPendingAuthorization(issuer, "stale"); ok {
		t.Fatal("get must reject past exp even if expiresAt is future")
	}
	if n, err := client.Exists(ctx, pendingStale).Result(); err != nil || n != 1 {
		t.Fatalf("expired get must not delete n=%d err=%v", n, err)
	}
}

func TestAuthorizationStore_GoUnmarshalFailureDoesNotBurn(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	authStore, _, _ := oauthredis.EphemeralStores(client, "test:")
	const issuer = "https://auth.example.test"
	seg := base64.RawURLEncoding.EncodeToString([]byte(issuer))
	raw, err := json.Marshal(map[string]any{
		"issuer":    issuer,
		"clientId":  "redis-client",
		"expiresAt": true,
		"exp":       time.Now().Add(time.Minute).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	codeKey := "test:authcode:" + seg + ":badjson"
	if err := client.Set(ctx, codeKey, raw, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.ConsumeAuthorizationCode(issuer, "badjson"); ok {
		t.Fatal("Go-invalid payload must not consume")
	}
	if n, err := client.Exists(ctx, codeKey).Result(); err != nil || n != 1 {
		t.Fatalf("expected auth code retained n=%d err=%v", n, err)
	}

	refreshKey := "test:refresh:" + seg + ":badjson"
	if err := client.HSet(ctx, refreshKey, "clientId", "redis-client", "payload", raw).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.LookupRefreshToken(issuer, "badjson"); ok {
		t.Fatal("Go-invalid refresh must not lookup")
	}
	if n, err := client.Exists(ctx, refreshKey).Result(); err != nil || n != 1 {
		t.Fatalf("lookup must not delete invalid refresh n=%d err=%v", n, err)
	}
	if _, ok := authStore.ConsumeRefreshToken(issuer, "badjson"); ok {
		t.Fatal("Go-invalid refresh must not consume")
	}
	if n, err := client.Exists(ctx, refreshKey).Result(); err != nil || n != 1 {
		t.Fatalf("expected refresh retained n=%d err=%v", n, err)
	}

	pendingKey := "test:pending:" + seg + ":badjson"
	if err := client.Set(ctx, pendingKey, raw, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := authStore.GetPendingAuthorization(issuer, "badjson"); ok {
		t.Fatal("Go-invalid pending must not get")
	}
	if n, err := client.Exists(ctx, pendingKey).Result(); err != nil || n != 1 {
		t.Fatalf("get must not delete invalid pending n=%d err=%v", n, err)
	}
	if _, ok := authStore.ConsumePendingAuthorization(issuer, "badjson"); ok {
		t.Fatal("Go-invalid pending must not consume")
	}
	if n, err := client.Exists(ctx, pendingKey).Result(); err != nil || n != 1 {
		t.Fatalf("expected pending retained n=%d err=%v", n, err)
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
