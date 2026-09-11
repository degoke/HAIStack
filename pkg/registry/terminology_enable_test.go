package registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

func TestEnsureTerminologyPackEnabledOptsInWithoutReinstall(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		GlobalTerminology:   global,
		TerminologyScope:    "tenant-a",
		TerminologyInstalls: installs,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:shared","version":"1","concept":[{"code":"a"}]}`)
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:shared", Version: "1", FHIRResourceType: "CodeSystem",
		PackageName: "shared-pack", PackageVersion: "1.0",
	}, nil)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: terminology.GlobalScopeID, ResourceType: "CodeSystem",
		CanonicalURL: "urn:shared", Version: "1", ResourceJSON: cs,
	})
	if err := mgr.EnsureTerminologyPackEnabled(ctx, "shared-pack", "1.0", "shared-pack"); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 1 || !installs.rows[0].Enabled {
		t.Fatalf("rows=%+v", installs.rows)
	}
}

func TestEnsureTerminologyPackEnabledRespectsOptOut(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		GlobalTerminology:   global,
		TerminologyInstalls: installs,
	})
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:shared","version":"1","concept":[{"code":"a"}]}`)
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:shared", Version: "1", FHIRResourceType: "CodeSystem",
		PackageName: "shared-pack", PackageVersion: "1.0",
	}, nil)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: terminology.GlobalScopeID, ResourceType: "CodeSystem",
		CanonicalURL: "urn:shared", Version: "1", ResourceJSON: cs,
	})
	_ = installs.SetEnabled(ctx, store.TerminologyInstallRecord{
		PackName: "shared-pack", PackVersion: "1.0",
		ResourceType: "CodeSystem", CanonicalURL: "urn:shared", Version: "1",
		Enabled: false,
	})
	if err := mgr.EnsureTerminologyPackEnabled(ctx, "shared-pack", "1.0", "shared-pack"); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 1 || installs.rows[0].Enabled {
		t.Fatalf("rows=%+v", installs.rows)
	}
}

func TestInstallDefinitionSkipsGlobalTerminologyReinstall(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		GlobalTerminology:   global,
		TerminologyScope:    "tenant-a",
		TerminologyInstalls: installs,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	cs := []byte(`{"resourceType":"CodeSystem","id":"cs1","url":"urn:shared","version":"1.0","concept":[{"code":"a"}]}`)
	provenance := registry.InstallProvenance{PackageName: "pack-a", PackageVersion: "1.0", SourceModule: "pack-a"}
	if err := mgr.InstallDefinition(ctx, cs, provenance); err != nil {
		t.Fatal(err)
	}
	if err := mgr.InstallDefinition(ctx, cs, provenance); err != nil {
		t.Fatal(err)
	}
	resources, err := global.ListResources(ctx, terminology.GlobalScopeID, "CodeSystem")
	if err != nil || len(resources) != 1 {
		t.Fatalf("global resources=%d err=%v", len(resources), err)
	}
}
