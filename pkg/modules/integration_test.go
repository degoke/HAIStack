package modules_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestSQLiteModuleInstallAndInspect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping module integration test in short mode")
	}
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "modules.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	mgr := newManagerFromDB(db.ModuleStore(), db.DefinitionStore(), db.RegistryInstallStore(), db.ResourceStore())
	runModuleInstallAndInspect(t, ctx, mgr)
}

func TestPostgresModuleInstallAndInspect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres module integration test in short mode")
	}
	ctx := context.Background()
	pgDB, cleanup := openPostgresTestDB(t)
	defer cleanup()
	if err := pgDB.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	const tenantID = "tenant-modules"
	if err := pgDB.EnsureTenant(ctx, tenantID); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	tenant := pgDB.Tenant(tenantID)
	mgr := newManagerFromDB(tenant.ModuleStore(), pgDB.DefinitionStore(), tenant.RegistryInstallStore(), tenant.ResourceStore())
	runModuleInstallAndInspect(t, ctx, mgr)
}

func newManagerFromDB(moduleStore store.ModuleStore, defs store.DefinitionStore, installs store.RegistryInstallStore, resources store.ResourceStore) *modules.Manager {
	reg := registry.NewManager(registry.Config{
		Definitions: defs,
		Installs:    installs,
	})
	return modules.NewManager(modules.Config{
		ModuleStore:          moduleStore,
		DefinitionStore:      defs,
		RegistryInstallStore: installs,
		RegistryManager:      reg,
		ResourceStore:        resources,
		Now:                  func() time.Time { return time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC) },
	})
}

func runModuleInstallAndInspect(t *testing.T, ctx context.Context, mgr *modules.Manager) {
	result, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if !result.Snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled")
	}

	installed, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(installed) != 1 || installed[0].Name != "core" {
		t.Fatalf("List = %+v, want one core module", installed)
	}
	if installed[0].Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", installed[0].Version)
	}
	if len(installed[0].Deferred.Permissions) != 1 || installed[0].Deferred.Permissions[0] != "read-patient" {
		t.Errorf("permissions = %v, want [read-patient]", installed[0].Deferred.Permissions)
	}
	if len(installed[0].Definitions) != 1 {
		t.Errorf("definitions = %d, want 1 search parameter (structure definitions are tracked separately)", len(installed[0].Definitions))
	}

	record, err := mgr.Inspect(ctx, "core")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if record.Name != "core" {
		t.Errorf("inspect name = %q", record.Name)
	}
}

func openPostgresTestDB(t *testing.T) (*postgres.DB, func()) {
	t.Helper()
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(context.Background(), dsn)
		if err != nil {
			t.Fatalf("Open TEST_POSTGRES_DSN: %v", err)
		}
		return db, db.Close
	}
	t.Skip("set TEST_POSTGRES_DSN for postgres module integration test")
	return nil, func() {}
}
