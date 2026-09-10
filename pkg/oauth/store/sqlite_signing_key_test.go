package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestLoadOrCreateRS256SignerPersistsAcrossCalls(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "signing-key.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	const issuer = "http://127.0.0.1:8080"
	first, err := store.LoadOrCreateRS256Signer(db.SQL(), issuer, "haistack")
	if err != nil {
		t.Fatal(err)
	}
	if first.PrivateKey == nil || first.Kid != "haistack" {
		t.Fatalf("first signer = %#v", first)
	}

	second, err := store.LoadOrCreateRS256Signer(db.SQL(), issuer, "haistack")
	if err != nil {
		t.Fatal(err)
	}
	if first.PrivateKey.N.Cmp(second.PrivateKey.N) != 0 {
		t.Fatal("expected same signing key after reload")
	}
	if second.Kid != "haistack" {
		t.Fatalf("kid = %q", second.Kid)
	}
}

func TestLoadOrCreateRS256SignerIgnoresTrailingSlash(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "signing-key-slash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	first, err := store.LoadOrCreateRS256Signer(db.SQL(), "http://host/", "haistack")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreateRS256Signer(db.SQL(), "http://host", "haistack")
	if err != nil {
		t.Fatal(err)
	}
	if first.PrivateKey.N.Cmp(second.PrivateKey.N) != 0 {
		t.Fatal("expected issuer normalization to reuse key")
	}
}
