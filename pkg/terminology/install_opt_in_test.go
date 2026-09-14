package terminology

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestEnsureInstallOptInRespectsExplicitOptOut(t *testing.T) {
	ctx := context.Background()
	installs := &memTerminologyInstallStore{}
	_ = installs.SetEnabled(ctx, store.TerminologyInstallRecord{
		CanonicalURL: "urn:vs", Version: "1", ResourceType: "ValueSet",
		Enabled: false, InstalledAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err := EnsureInstallOptIn(ctx, installs, store.TerminologyInstallRecord{
		CanonicalURL: "urn:vs", Version: "1", ResourceType: "ValueSet", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 1 || installs.rows[0].Enabled {
		t.Fatalf("rows=%+v", installs.rows)
	}
}

func TestEnsureInstallOptInPreservesInstalledAt(t *testing.T) {
	ctx := context.Background()
	installs := &memTerminologyInstallStore{}
	installedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = installs.SetEnabled(ctx, store.TerminologyInstallRecord{
		CanonicalURL: "urn:vs", Version: "1", ResourceType: "ValueSet",
		Enabled: true, InstalledAt: installedAt,
	})
	if err := EnsureInstallOptIn(ctx, installs, store.TerminologyInstallRecord{
		CanonicalURL: "urn:vs", Version: "1", ResourceType: "ValueSet", Enabled: true,
		InstalledAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if !installs.rows[0].InstalledAt.Equal(installedAt) {
		t.Fatalf("installedAt=%v want %v", installs.rows[0].InstalledAt, installedAt)
	}
}

func TestEnsureInstallOptInOptsInMultipleResources(t *testing.T) {
	ctx := context.Background()
	installs := &memTerminologyInstallStore{}
	for _, url := range []string{"urn:vs1", "urn:vs2"} {
		if err := EnsureInstallOptIn(ctx, installs, store.TerminologyInstallRecord{
			ResourceType: "ValueSet", CanonicalURL: url, Version: "1", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(installs.rows) != 2 {
		t.Fatalf("rows=%d want 2", len(installs.rows))
	}
}

func TestEnsureCatalogPackOptInRespectsOptOut(t *testing.T) {
	ctx := context.Background()
	global := NewMemoryStore()
	defs := &memDefinitionStore{}
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","status":"active"}`)
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "pack-a", PackageVersion: "1.0",
	}, nil)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet",
		CanonicalURL: "urn:vs", Version: "1", ResourceJSON: vsJSON,
	})
	installs := &memTerminologyInstallStore{}
	_ = installs.SetEnabled(ctx, store.TerminologyInstallRecord{
		PackName: "pack-a", PackVersion: "1.0",
		CanonicalURL: "urn:vs", Version: "1", ResourceType: "ValueSet",
		Enabled: false,
	})
	if err := EnsureCatalogPackOptIn(ctx, CatalogEnableOptions{
		Global: global, Installs: installs, Definitions: defs,
	}, "pack-a", "1.0", "pack-a"); err != nil {
		t.Fatal(err)
	}
	if installs.rows[0].Enabled {
		t.Fatal("expected opt-out to be preserved")
	}
}
