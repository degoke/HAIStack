package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveViewDataDirUsesAbsolutePaths(t *testing.T) {
	rel := "relative-view-data"
	b := New().WithDataDir(rel)
	got, err := b.resolveViewDataDir(&wireState{})
	if err != nil {
		t.Fatalf("resolveViewDataDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
	if !strings.HasSuffix(got, rel) {
		t.Fatalf("path=%q should end with %q", got, rel)
	}
}

func TestResolveViewDataDirSQLiteSiblingDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "runtime.db")
	b := New().WithSQLite(dbPath)
	got, err := b.resolveViewDataDir(&wireState{})
	if err != nil {
		t.Fatalf("resolveViewDataDir: %v", err)
	}
	wantSuffix := filepath.Join("nested", "view-exports")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("path=%q want suffix %q", got, wantSuffix)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestResolveViewExportJobStoreInMemoryWithoutDataDir(t *testing.T) {
	b := New()
	store, err := b.resolveViewExportJobStore(&wireState{})
	if err != nil {
		t.Fatalf("resolveViewExportJobStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected in-memory export job store")
	}
}
