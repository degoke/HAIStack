package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/oauth/store"
	"github.com/degoke/haistack/pkg/sqlite"
)

func TestSQLiteRateLimitStoreAllow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth-rate-limit.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	limiter := &store.SQLiteRateLimitStore{DB: db.SQL(), Now: func() time.Time { return now }}
	window := time.Minute
	endpoint := "token"
	bucket := "127.0.0.1"

	allowed, err := limiter.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil || !allowed {
		t.Fatalf("first allow = %v err = %v", allowed, err)
	}
	allowed, err = limiter.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil || !allowed {
		t.Fatalf("second allow = %v err = %v", allowed, err)
	}
	allowed, err = limiter.Allow(ctx, endpoint, bucket, 2, window, now)
	if err != nil {
		t.Fatalf("third allow err = %v", err)
	}
	if allowed {
		t.Fatal("third should be denied")
	}
}
