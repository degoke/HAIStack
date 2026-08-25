package sqlite_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestOpenAndMigrate(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, t.TempDir()+"/haistack.db")
	if err != nil {
		t.Fatalf("OpenAndMigrate: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate should be idempotent: %v", err)
	}
}
