package registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

type tenantInstallFactory struct {
	stores map[string]*memTerminologyInstallStore
}

func (f *tenantInstallFactory) ForTenant(_ context.Context, tenantID string) (store.TerminologyInstallStore, error) {
	if f.stores == nil {
		f.stores = make(map[string]*memTerminologyInstallStore)
	}
	if f.stores[tenantID] == nil {
		f.stores[tenantID] = &memTerminologyInstallStore{}
	}
	return f.stores[tenantID], nil
}

func TestContextWithJobTerminologyInstallsOptsInOwnerTenant(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	global := terminology.NewMemoryStore()
	defaultInstalls := &memTerminologyInstallStore{}
	factory := &tenantInstallFactory{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		GlobalTerminology:   global,
		TerminologyInstalls: defaultInstalls,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	cs := []byte(`{"resourceType":"CodeSystem","id":"cs1","url":"urn:tenant-cs","version":"1.0","concept":[{"code":"a"}]}`)
	payload, err := jobs.MarshalPayload(jobs.PackageInstallPayload{Source: "registry", PackageID: "pack-a", Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = jobs.StampOwner(payload, jobs.JobOwner{PrincipalID: "user-b", TenantID: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	job := store.JobRecord{Payload: payload}
	ctx, err = registry.ContextWithJobTerminologyInstalls(ctx, job, factory, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.InstallDefinition(ctx, cs, registry.InstallProvenance{
		PackageName: "pack-a", PackageVersion: "1.0", SourceModule: "pack-a",
	}); err != nil {
		t.Fatal(err)
	}
	if len(defaultInstalls.rows) != 0 {
		t.Fatalf("default tenant rows=%+v", defaultInstalls.rows)
	}
	tenantB := factory.stores["tenant-b"]
	if tenantB == nil || len(tenantB.rows) != 1 || !tenantB.rows[0].Enabled {
		t.Fatalf("tenant-b rows=%+v", tenantB)
	}
}
