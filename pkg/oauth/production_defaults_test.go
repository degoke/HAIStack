package oauth_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth"
)

func TestApplyProductionDefaults(t *testing.T) {
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "test-encryption-secret")
	cfg := oauth.Config{
		Issuer:                   "https://auth.example.test",
		AllowDynamicRegistration: true,
		RegistrationAccessToken:  "register-token",
	}
	if err := oauth.ApplyProductionDefaults(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.RequirePKCEForAllClients {
		t.Fatal("expected PKCE required for all clients")
	}
}

func TestApplyProductionDefaultsRejectsAutoApprove(t *testing.T) {
	t.Setenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET", "test-encryption-secret")
	cfg := oauth.Config{
		Issuer:      "https://auth.example.test",
		AutoApprove: true,
	}
	if err := oauth.ApplyProductionDefaults(&cfg); err == nil {
		t.Fatal("expected auto approve rejection")
	}
}

func TestDefaultSigningKeyPaths(t *testing.T) {
	paths := oauth.DefaultSigningKeyPaths("/var/lib/haistack")
	if paths.SigningKey != "/var/lib/haistack/oauth-signing.pem" {
		t.Fatalf("signing key path = %q", paths.SigningKey)
	}
}

func TestSigningKeyRotateOnStartupEnv(t *testing.T) {
	t.Setenv("OAUTH_SIGNING_KEY_ROTATE", "1")
	if !oauth.SigningKeyRotateOnStartup() {
		t.Fatal("expected rotate on startup")
	}
	t.Setenv("OAUTH_SIGNING_KEY_ROTATE", "")
	if oauth.SigningKeyRotateOnStartup() {
		t.Fatal("expected rotate disabled")
	}
}
