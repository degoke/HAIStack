package registry_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestPackageVersionInstalled(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	mgr := registry.NewManager(registry.Config{
		Definitions: defs,
		Installs:    newMemInstallStore(),
	})
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "test.ig", PackageVersion: "2.0.0",
	}, nil)
	ok, err := mgr.PackageVersionInstalled(ctx, "test.ig", "2.0.0")
	if err != nil || !ok {
		t.Fatalf("installed=%v err=%v", ok, err)
	}
	ok, err = mgr.PackageVersionInstalled(ctx, "test.ig", "1.0.0")
	if err != nil || ok {
		t.Fatalf("other version installed=%v err=%v", ok, err)
	}
}
