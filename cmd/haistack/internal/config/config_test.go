package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
)

func TestDefaultsValidate(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Storage.Driver != config.DriverSQLite {
		t.Fatalf("driver = %q", cfg.Storage.Driver)
	}
}

func TestLoadFromFileAndFlagOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "haistack.yaml")
	if err := os.WriteFile(path, []byte(`
storage:
  driver: sqlite
  sqlitePath: custom.db
runtime:
  enableSearch: false
  modulePaths: []
sync:
  nodeID: file-node
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	driver := config.DriverSQLite
	sqlitePath := "flag.db"
	enableSearch := true
	cfg, err := config.Load(path, config.Overrides{
		SQLitePath:    &sqlitePath,
		EnableSearch:  &enableSearch,
		StorageDriver: &driver,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLitePath != "flag.db" {
		t.Fatalf("sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if !cfg.Runtime.EnableSearch {
		t.Fatal("expected enableSearch override true")
	}
	if cfg.Sync.NodeID != "file-node" {
		t.Fatalf("node id = %q", cfg.Sync.NodeID)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haistack.yaml")
	if err := os.WriteFile(path, config.StarterYAML(), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HAISTACK_SQLITE_PATH", "env.db")
	t.Setenv("HAISTACK_SYNC_NODE_ID", "env-node")

	cfg, err := config.Load(path, config.Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLitePath != "env.db" {
		t.Fatalf("sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if cfg.Sync.NodeID != "env-node" {
		t.Fatalf("node id = %q", cfg.Sync.NodeID)
	}
}

func TestLoadUsesDefaultsWhenDefaultFileIsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, config.DefaultConfigFile)

	cfg, err := config.Load(path, config.Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Driver != config.DriverSQLite {
		t.Fatalf("driver = %q", cfg.Storage.Driver)
	}
	if cfg.Storage.SQLitePath != config.DefaultSQLitePath {
		t.Fatalf("sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if cfg.Storage.SQLiteTenantID != config.DefaultSQLiteTenantID || cfg.Storage.SQLiteTerminologyScope != config.DefaultSQLiteTerminologyScope {
		t.Fatalf("sqlite namespaces = %q/%q", cfg.Storage.SQLiteTenantID, cfg.Storage.SQLiteTerminologyScope)
	}
	if len(cfg.Runtime.ModulePaths) != 0 {
		t.Fatalf("default module paths = %#v, want empty", cfg.Runtime.ModulePaths)
	}
}

func TestInvalidEnableSearchEnvironmentFails(t *testing.T) {
	t.Setenv("HAISTACK_ENABLE_SEARCH", "sometimes")
	if _, err := config.Load(config.DefaultConfigFile, config.Overrides{}); err == nil {
		t.Fatal("expected invalid HAISTACK_ENABLE_SEARCH error")
	}
}

func TestSQLiteNamespaceOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haistack.yaml")
	if err := os.WriteFile(path, config.StarterYAML(), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HAISTACK_SQLITE_TENANT_ID", "device-a")
	t.Setenv("HAISTACK_SQLITE_TERMINOLOGY_SCOPE", "scope-a")

	tenantID := "device-b"
	scope := "scope-b"
	cfg, err := config.Load(path, config.Overrides{
		SQLiteTenantID:         &tenantID,
		SQLiteTerminologyScope: &scope,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLiteTenantID != tenantID || cfg.Storage.SQLiteTerminologyScope != scope {
		t.Fatalf("sqlite namespaces = %q/%q", cfg.Storage.SQLiteTenantID, cfg.Storage.SQLiteTerminologyScope)
	}
}

func TestLoadResolvesRelativePathsFromConfigLocation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(cfgDir, "haistack.yaml")
	if err := os.WriteFile(path, []byte(`
storage:
  driver: sqlite
  sqlitePath: .haistack/custom.db
runtime:
  modulePaths:
    - ../modules/core
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path, config.Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLitePath != filepath.Join(cfgDir, ".haistack", "custom.db") {
		t.Fatalf("sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if got, want := cfg.Runtime.ModulePaths[0], filepath.Join(cfgDir, "../modules/core"); got != want {
		t.Fatalf("module path = %q, want %q", got, want)
	}
}

func TestPostgresRequiresTenant(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Storage.Driver = config.DriverPostgres
	cfg.Storage.PostgresDSN = "postgres://example"
	cfg.Normalize()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected tenant validation error")
	}
}
