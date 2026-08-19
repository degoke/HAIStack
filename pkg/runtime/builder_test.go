package runtime_test

import (
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/runtime"
)

func TestBuilderValidationConflictingStorage(t *testing.T) {
	_, err := runtime.New().
		WithSQLite("/tmp/a.db").
		WithPostgresAllInOne("postgres://localhost/db", "tenant-a").
		Build(t.Context())
	if !errors.Is(err, runtime.ErrConflictingStorage) {
		t.Fatalf("expected ErrConflictingStorage, got %v", err)
	}
}

func TestBuilderValidationNoStorage(t *testing.T) {
	_, err := runtime.New().Build(t.Context())
	if !errors.Is(err, runtime.ErrNoStorage) {
		t.Fatalf("expected ErrNoStorage, got %v", err)
	}
}

func TestBuilderValidationMissingTenantID(t *testing.T) {
	_, err := runtime.New().
		WithPostgresAllInOne("postgres://localhost/db", "").
		Build(t.Context())
	if !errors.Is(err, runtime.ErrMissingTenantID) {
		t.Fatalf("expected ErrMissingTenantID, got %v", err)
	}
}

func TestBuilderValidationExternalAdapterInSQLite(t *testing.T) {
	_, err := runtime.New().
		WithSQLite("/tmp/a.db").
		WithExternalBlobStore(runtime.TestNoopBlobStore()).
		Build(t.Context())
	if !errors.Is(err, runtime.ErrExternalAdapterInSQLite) {
		t.Fatalf("expected ErrExternalAdapterInSQLite, got %v", err)
	}
}

func TestModeResolutionLocalSQLite(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/test.db").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()
	if rt.Mode() != runtime.ModeLocalSQLite {
		t.Fatalf("mode = %q, want %q", rt.Mode(), runtime.ModeLocalSQLite)
	}
}

func TestModeResolutionEdgePostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres mode resolution in short mode")
	}
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, "tenant-mode-edge").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()
	if rt.Mode() != runtime.ModeEdgePostgresAllInOne {
		t.Fatalf("mode = %q, want %q", rt.Mode(), runtime.ModeEdgePostgresAllInOne)
	}
}

func TestModeResolutionCloudPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres mode resolution in short mode")
	}
	dsn, cleanup := openPostgresDSN(t)
	defer cleanup()

	rt, err := runtime.New().
		WithPostgresAllInOne(dsn, "tenant-mode-cloud").
		WithExternalSearch(runtime.TestNoopExternalSearch()).
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()
	if rt.Mode() != runtime.ModeCloudPostgresPlusExternalServices {
		t.Fatalf("mode = %q, want %q", rt.Mode(), runtime.ModeCloudPostgresPlusExternalServices)
	}
	if rt.Services().ExternalSearch == nil {
		t.Fatal("expected external search adapter in services")
	}
}

func TestStartTwiceReturnsError(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/start.db").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()

	if err := rt.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Start(t.Context()); !errors.Is(err, runtime.ErrAlreadyStarted) {
		t.Fatalf("expected ErrAlreadyStarted, got %v", err)
	}
}

func TestConfigNormalization(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/cfg.db").
		WithSearch().
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()

	cfg := rt.Config()
	if cfg.Mode != runtime.ModeLocalSQLite {
		t.Fatalf("config mode = %q", cfg.Mode)
	}
	if !cfg.SearchEnabled {
		t.Fatal("expected search enabled in config")
	}
}

func TestSQLiteNamespaceConfiguration(t *testing.T) {
	rt, err := runtime.New().
		WithSQLite(t.TempDir() + "/namespaces.db").
		WithSQLiteTenant("device-a").
		WithSQLiteTerminologyScope("tenant-a").
		Build(t.Context())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { _ = rt.Shutdown(t.Context()) }()
	cfg := rt.Config()
	if cfg.SQLiteTenantID != "device-a" || cfg.SQLiteTerminologyScope != "tenant-a" {
		t.Fatalf("SQLite namespaces = %q/%q, want device-a/tenant-a", cfg.SQLiteTenantID, cfg.SQLiteTerminologyScope)
	}
}
