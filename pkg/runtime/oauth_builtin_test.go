package runtime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestBuiltinOAuthWiresDiscovery(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime.db")
	rt, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:0").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			TenantID: "local",
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

func TestBuiltinOAuthProductionRequiresRegistrationToken(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth-runtime-prod.db")
	_, err := runtime.New().
		WithSQLite(dbPath).
		WithHTTP("127.0.0.1:8080").
		WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
			Production: true,
			TenantID:   "local",
		}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected production defaults to require registration token")
	}
}
