package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestPackageInstallStoreMarkCompleteIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := db.PackageInstallStore()
	first := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	second := first.Add(24 * time.Hour)
	if err := store.MarkComplete(ctx, "test.ig", "1.0.0", first); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkComplete(ctx, "test.ig", "1.0.0", second); err != nil {
		t.Fatal(err)
	}
	ok, err := store.IsComplete(ctx, "test.ig", "1.0.0")
	if err != nil || !ok {
		t.Fatalf("complete=%v err=%v", ok, err)
	}
}
