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

	"github.com/degoke/haistack/pkg/runtime"
	"github.com/degoke/haistack/pkg/sqlite"
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
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "oauth-persist.db")

	fetchJWKS := func() string {
		rt, err := runtime.New().
			WithSQLite(dbPath).
			WithHTTP("127.0.0.1:0").
			WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
				IssuerURL: "http://127.0.0.1:8080",
				TenantID:  "local",
				StateDir:  filepath.Join(dir, "oauth"),
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

		resp, err := http.Get(ts.URL + "/oauth/jwks")
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

func TestBuiltinOAuthTenantRoute(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-tenant.db")
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

	resp, err := http.Get(ts.URL + "/t/local/.well-known/smart-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestBuiltinOAuthProductionRequiresLoginUsers(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-prod-users.db")
	t.Setenv("OAUTH_REGISTRATION_TOKEN", "register-token")
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "signing-secret")
	t.Setenv("OAUTH_SESSION_SECRET", "session-secret")
	_, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			Production:              true,
			IssuerURL:               "https://auth.example.test",
			RegistrationAccessToken: "register-token",
			TenantID:                "local",
		}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected production without login users to fail")
	}
	if !strings.Contains(err.Error(), "OAUTH_LOGIN_USERS") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltinOAuthProductionOmitsLoopbackClient(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-prod-client.db")
	t.Setenv("OAUTH_REGISTRATION_TOKEN", "register-token")
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "signing-secret")
	t.Setenv("OAUTH_SESSION_SECRET", "session-secret")
	t.Setenv("OAUTH_LOGIN_USERS", "clinician-1:s3cret")
	rt, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			Production:              true,
			IssuerURL:               "https://auth.example.test",
			RegistrationAccessToken: "register-token",
			TenantID:                "local",
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

	resp, err := http.Get(ts.URL + "/oauth/authorize?client_id=haistack-app&redirect_uri=http://127.0.0.1/callback&response_type=code&scope=openid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("haistack-app status = %d", resp.StatusCode)
	}
}

func TestBuiltinOAuthRebindsMigratedClientToTenant(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-rebind.db")
	db, err := sqlite.OpenAndMigrate(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO hai_oauth_client (issuer, client_id, payload, updated_at)
		VALUES ('', 'legacy-app', ?, datetime('now'))`,
		`{"ClientID":"legacy-app","RedirectURIs":["https://app.example/cb"],"Scopes":["openid"]}`,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

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

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/t/local/oauth/authorize?client_id=legacy-app&redirect_uri=https://app.example/cb&response_type=code&scope=openid&code_challenge=abc&code_challenge_method=S256")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("tenant authorize status = %d body=%s", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "code=") {
		t.Fatalf("expected authorization code, Location=%q", loc)
	}
}

func TestBuiltinOAuthProductionRejectsHTTPDerivedIssuer(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-http-prod.db")
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
