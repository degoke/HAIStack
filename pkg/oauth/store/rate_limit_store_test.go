package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestApplySQLStoresPostgresDialect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-postgres-dialect.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := oauth.Config{Issuer: "https://example.com"}
	if err := applySQLStores(&cfg, WrapSQLDB(db.SQL(), DialectPostgres), DialectPostgres); err != nil {
		t.Fatalf("applySQLStores: %v", err)
	}
	if cfg.CodeStore == nil || cfg.ClientStore == nil || cfg.RateLimitStore == nil {
		t.Fatalf("expected stores wired, got code=%v client=%v rate=%v", cfg.CodeStore, cfg.ClientStore, cfg.RateLimitStore)
	}
}

func TestSQLRateLimitStoreAllow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-rate-limit.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wrapped := WrapSQLDB(db.SQL(), DialectSQLite)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := &SQLRateLimitStore{DB: wrapped, Dialect: DialectSQLite, Now: func() time.Time { return now }}
	window := time.Minute
	endpoint := "token"
	bucket := "127.0.0.1"

	allowed, err := store.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil || !allowed {
		t.Fatalf("first allow = %v err = %v", allowed, err)
	}
	allowed, err = store.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil || !allowed {
		t.Fatalf("second allow = %v err = %v", allowed, err)
	}
	allowed, err = store.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil {
		t.Fatalf("third allow err = %v", err)
	}
	if allowed {
		t.Fatal("third should be denied")
	}

	other := &SQLRateLimitStore{DB: wrapped, Dialect: DialectSQLite, Now: func() time.Time { return now }}
	allowed, err = other.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil {
		t.Fatalf("shared store err = %v", err)
	}
	if allowed {
		t.Fatal("shared DB counter should deny third request from another store instance")
	}
}
