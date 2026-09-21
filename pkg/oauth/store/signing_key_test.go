package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestLoadOrCreateSQLiteSigningKeySet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-signing-key.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	issuer := "https://auth.example.test"
	set, err := store.LoadOrCreateSQLiteSigningKeySet(db.SQL(), issuer, store.SigningKeyOptions{
		ActiveKeyID: "haistack",
	})
	if err != nil {
		t.Fatalf("load signing key: %v", err)
	}
	if set.Active == nil || set.Active.PrivateKey == nil {
		t.Fatal("expected active signing key")
	}

	reloaded, err := store.LoadOrCreateSQLiteSigningKeySet(db.SQL(), issuer, store.SigningKeyOptions{
		ActiveKeyID: "haistack",
	})
	if err != nil {
		t.Fatalf("reload signing key: %v", err)
	}
	if reloaded.Active.KeyID != set.Active.KeyID {
		t.Fatalf("key id changed: %q vs %q", reloaded.Active.KeyID, set.Active.KeyID)
	}

	rotated, err := store.LoadOrCreateSQLiteSigningKeySet(db.SQL(), issuer, store.SigningKeyOptions{
		ActiveKeyID:     "haistack",
		RotateOnStartup: true,
	})
	if err != nil {
		t.Fatalf("rotate signing key: %v", err)
	}
	if rotated.Active.KeyID == set.Active.KeyID {
		t.Fatal("expected rotated key id")
	}
	foundPrev := false
	for _, key := range rotated.Verification {
		if key != nil && key.KeyID == set.Active.KeyID {
			foundPrev = true
			break
		}
	}
	if !foundPrev {
		t.Fatal("expected previous signing key to remain in JWKS verification set")
	}
}
