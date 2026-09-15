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
}
