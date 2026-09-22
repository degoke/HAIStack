package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/postgres"
	"github.com/degoke/haistack/pkg/registry"
	"github.com/degoke/haistack/pkg/sqlite"
	"github.com/degoke/haistack/pkg/store"
)

func TestSQLiteRegistryStores(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	definitions := db.DefinitionStore()
	installs := db.RegistryInstallStore()
	runRegistryStoreTests(t, ctx, definitions, installs)
}

func TestPostgresRegistryHybridScoping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}

	ctx := context.Background()
	pgDB, cleanup := openPostgresTestDB(t)
	defer cleanup()
	if err := pgDB.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := pgDB.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	globalDefinitions := pgDB.DefinitionStore()
	tenantA := pgDB.Tenant("tenant-a")
	tenantB := pgDB.Tenant("tenant-b")
	if err := pgDB.EnsureTenant(ctx, "tenant-a"); err != nil {
		t.Fatalf("EnsureTenant a: %v", err)
	}
	if err := pgDB.EnsureTenant(ctx, "tenant-b"); err != nil {
		t.Fatalf("EnsureTenant b: %v", err)
	}

	runRegistryStoreTests(t, ctx, globalDefinitions, tenantA.RegistryInstallStore())

	if err := tenantA.RegistryInstallStore().SetEnabled(ctx, store.RegistryInstallRecord{
		DefinitionKind:     store.DefinitionKindStructureDefinition,
		CanonicalURL:       "http://hl7.org/fhir/StructureDefinition/Patient",
		Version:            "4.0.1",
		TargetResourceType: "Patient",
		Enabled:            true,
		InstalledAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("tenant A enable: %v", err)
	}
	enabledB, err := tenantB.RegistryInstallStore().ListEnabled(ctx)
	if err != nil {
		t.Fatalf("tenant B list enabled: %v", err)
	}
	if len(enabledB) != 0 {
		t.Fatalf("tenant B should have no enablement, got %d rows", len(enabledB))
	}
	enabledA, err := tenantA.RegistryInstallStore().ListEnabled(ctx)
	if err != nil {
		t.Fatalf("tenant A list enabled: %v", err)
	}
	if len(enabledA) != 1 {
		t.Fatalf("tenant A enabled rows = %d, want 1", len(enabledA))
	}
}

func runRegistryStoreTests(t *testing.T, ctx context.Context, definitions store.DefinitionStore, installs store.RegistryInstallStore) {
	t.Helper()

	manager := registry.NewManager(registry.Config{
		Definitions: definitions,
		Installs:    installs,
	})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("second SeedBundled should be idempotent: %v", err)
	}
	record, err := definitions.Get(ctx, "http://hl7.org/fhir/StructureDefinition/Patient", "4.0.1")
	if err != nil {
		t.Fatalf("Get Patient SD: %v", err)
	}
	if record.DefinitionKind != store.DefinitionKindStructureDefinition {
		t.Fatalf("kind = %q", record.DefinitionKind)
	}
	filtered, err := definitions.List(ctx, store.DefinitionFilter{
		DefinitionKind:     store.DefinitionKindSearchParameter,
		TargetResourceType: "Patient",
	})
	if err != nil {
		t.Fatalf("List Patient search params: %v", err)
	}
	if len(filtered) < 3 {
		t.Fatalf("Patient search params = %d, want at least 3", len(filtered))
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if !snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled")
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
	t.Skip("set TEST_POSTGRES_DSN for postgres registry integration test")
	return nil, func() {}
}
