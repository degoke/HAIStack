package runtime_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestBuiltinOAuthWiresDiscovery(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime.db")
	rt, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			IssuerURL: "http://127.0.0.1:8080",
			TenantID:  "local",
		}).
		Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Shutdown(ctx) }()

	ts := httptest.NewServer(rt.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/fhir/.well-known/smart-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["authorization_endpoint"] == "" || doc["token_endpoint"] == "" {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestBuiltinOAuthRequiresIssuerWhenListenPortZero(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-port-zero.db")
	_, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:0").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			TenantID: "local",
		}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected port 0 without issuer URL to fail")
	}
	if !strings.Contains(err.Error(), "IssuerURL") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltinOAuthProductionRequiresRegistrationToken(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-prod.db")
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "test-signing-key-secret")
	_, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			Production: true,
			IssuerURL:  "https://auth.example.test",
			TenantID:   "local",
		}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected production defaults to require registration token")
	}
	if !strings.Contains(err.Error(), "OAUTH_REGISTRATION_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltinOAuthPersistsSigningKey(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-persist.db")

	fetchJWKS := func() string {
		rt, err := runtime.New().
			WithSQLite(dbPath).
			WithHTTP("127.0.0.1:0").
			WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
				IssuerURL: "http://127.0.0.1:8080",
				TenantID:  "local",
			}).
			Build(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rt.Shutdown(ctx) }()

		ts := httptest.NewServer(rt.Handler())
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/.well-known/jwks.json")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	first := fetchJWKS()
	second := fetchJWKS()
	if first == "" || first != second {
		t.Fatalf("jwks changed across restarts:\nfirst=%q\nsecond=%q", first, second)
	}
}

func TestBuiltinOAuthProductionRejectsHTTPDerivedIssuer(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-http-prod.db")
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "test-signing-key-secret")
	t.Setenv("OAUTH_REGISTRATION_TOKEN", "register-token")
	_, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			Production: true,
			TenantID:   "local",
		}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected production without pinned issuer to fail")
	}
}
