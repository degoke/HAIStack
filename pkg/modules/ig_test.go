package modules_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestInstallCompiledIGArtefacts(t *testing.T) {
	igDir := filepath.Join("..", "..", "modules", "core", "ig")
	defs, err := modules.LoadDefinitionsFromIG(igDir)
	if err != nil {
		t.Fatalf("LoadDefinitionsFromIG: %v", err)
	}
	if len(defs) < 2 {
		t.Fatalf("compiled core IG definitions = %d, want at least SearchParameter + StructureDefinition", len(defs))
	}

	loader := modules.NewLoader()
	mod, err := loader.Load(filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("Load core: %v", err)
	}
	if mod.Manifest.IGPackage != "ig" {
		t.Fatalf("igPackage = %q", mod.Manifest.IGPackage)
	}

	var sawSearch, sawProfile bool
	for _, def := range mod.Definitions {
		parsed, _, err := registry.ParseDefinition(def)
		if err != nil {
			t.Fatalf("ParseDefinition: %v", err)
		}
		switch parsed.DefinitionKind {
		case store.DefinitionKindSearchParameter:
			if parsed.CanonicalURL == "http://haistack.example.org/SearchParameter/Patient-identifier-custom" {
				sawSearch = true
			}
		case store.DefinitionKindStructureDefinition:
			if parsed.CanonicalURL == "http://haistack.example.org/fhir/StructureDefinition/hai-patient" {
				sawProfile = true
			}
		}
	}
	if !sawSearch || !sawProfile {
		t.Fatalf("core IG missing search=%v profile=%v", sawSearch, sawProfile)
	}

	ctx := context.Background()
	defsStore := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	modStore := newMemModuleStore()
	reg := registry.NewManager(registry.Config{Definitions: defsStore, Installs: installs})
	mgr := modules.NewManager(modules.Config{
		ModuleStore:          modStore,
		DefinitionStore:      defsStore,
		RegistryInstallStore: installs,
		RegistryManager:      reg,
		Now:                  func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	result, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("Install compiled core IG: %v", err)
	}
	if !result.Snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled from compiled IG module")
	}

	// Direct IG directory load matches module igPackage contents.
	if len(defs) != len(mod.Definitions) {
		t.Fatalf("LoadDefinitionsFromIG=%d module definitions=%d", len(defs), len(mod.Definitions))
	}
	found := false
	for _, def := range defs {
		if bytes.Contains(def, []byte("hai-patient")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected hai-patient in compiled IG directory")
	}
}
