package packages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestInstallWorkerOptsInJobOwnerTenant(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "worker-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	defaultTenant := "tenant-a"
	mgr := registry.NewManager(registry.Config{
		Definitions:         db.DefinitionStore(),
		Installs:            db.RegistryInstallStore(),
		GlobalTerminology:   db.GlobalTerminologyStore(),
		TerminologyInstalls: db.TerminologyInstallStore(defaultTenant),
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	installer := &packages.Installer{Registry: mgr}
	worker := &packages.InstallWorker{
		Installer:                  installer,
		TerminologyInstalls:        sqlite.NewTerminologyInstallStoreFactory(db),
		DefaultTerminologyTenantID: defaultTenant,
	}

	packageDir := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"demo.terminology","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := []byte(`{
		"resourceType":"CodeSystem",
		"id":"cs1",
		"url":"urn:worker-cs",
		"version":"1.0",
		"status":"active",
		"concept":[{"code":"a","display":"A"}]
	}`)
	if err := os.WriteFile(filepath.Join(packageDir, "codesystem.json"), cs, 0o644); err != nil {
		t.Fatal(err)
	}

	payload, err := jobs.MarshalPayload(jobs.PackageInstallPayload{
		Source: "path",
		Path:   packageDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = jobs.StampOwner(payload, jobs.JobOwner{PrincipalID: "user-b", TenantID: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	job := store.JobRecord{Type: jobs.TypeRegistryPackageInstall, Payload: payload}

	if err := worker.HandleJob(ctx, job); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	tenantA := db.TerminologyInstallStore(defaultTenant)
	tenantB := db.TerminologyInstallStore("tenant-b")
	aRows, err := tenantA.ListInstalled(ctx, store.TerminologyInstallFilter{})
	if err != nil {
		t.Fatal(err)
	}
	bRows, err := tenantB.ListInstalled(ctx, store.TerminologyInstallFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(aRows) != 0 {
		t.Fatalf("tenant-a installs=%+v", aRows)
	}
	if len(bRows) != 1 || !bRows[0].Enabled || bRows[0].CanonicalURL != "urn:worker-cs" {
		t.Fatalf("tenant-b installs=%+v", bRows)
	}
}
