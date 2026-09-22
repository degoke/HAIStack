package oauth_test

import (
	"path/filepath"
	"testing"

	"github.com/degoke/haistack/pkg/oauth"
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
	dir := t.TempDir()
	paths := oauth.DefaultSigningKeyPaths(dir)
	want := filepath.Join(dir, "oauth-signing.pem")
	if paths.SigningKey != want {
		t.Fatalf("signing key path = %q want %q", paths.SigningKey, want)
	}
	if paths.SigningKID != "haistack" {
		t.Fatalf("signing kid = %q", paths.SigningKID)
	}
}

func TestLoadSigningKey_RoundTripPEM(t *testing.T) {
	paths := oauth.DefaultSigningKeyPaths(t.TempDir())
	created, err := oauth.LoadOrCreateSigningKey(paths.SigningKey, paths.SigningKID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := oauth.LoadSigningKey(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.KeyID != created.KeyID {
		t.Fatalf("loaded = %+v created kid=%q", loaded, created.KeyID)
	}
	missing, err := oauth.LoadSigningKey(oauth.SigningKeyPaths{SigningKey: filepath.Join(t.TempDir(), "missing.pem")})
	if err != nil || missing != nil {
		t.Fatalf("missing key = %v err=%v", missing, err)
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
