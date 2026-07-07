package modules_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestRegistryApplierInstallsDefinitionAndRebuildsSnapshot(t *testing.T) {
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	applier := modules.NewRegistryApplier(reg, defs, installs)
	ctx := context.Background()

	if err := applier.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := applier.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}

	raw := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/custom-patient-tag",
		"version":"1.0.0",
		"name":"tag",
		"status":"active",
		"code":"tag",
		"base":["Patient"],
		"type":"token",
		"expression":"Patient.meta.tag"
	}`)
	if err := applier.InstallDefinition(ctx, raw, registry.InstallProvenance{ModuleName: "tags", SourceModule: "tags"}); err != nil {
		t.Fatalf("InstallDefinition: %v", err)
	}

	snapshot, err := applier.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if !snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled")
	}
	params := snapshot.SearchParametersFor("Patient")
	found := false
	for _, p := range params {
		if p.Code == "tag" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom tag search parameter should be in snapshot")
	}
}

func TestRegistryApplierDeleteInstallIsPrecise(t *testing.T) {
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	applier := modules.NewRegistryApplier(reg, defs, installs)
	ctx := context.Background()

	// Install two versions of the same search parameter for Patient.
	v1 := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/patient-tag",
		"version":"1.0.0","name":"tag","status":"active","code":"tag",
		"base":["Patient"],"type":"token","expression":"Patient.meta.tag"
	}`)
	v2 := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/patient-tag",
		"version":"2.0.0","name":"tag","status":"active","code":"tag",
		"base":["Patient"],"type":"token","expression":"Patient.meta.tag"
	}`)
	if err := applier.InstallDefinition(ctx, v1, registry.InstallProvenance{ModuleName: "m", SourceModule: "m"}); err != nil {
		t.Fatalf("Install v1: %v", err)
	}
	if err := applier.InstallDefinition(ctx, v2, registry.InstallProvenance{ModuleName: "m", SourceModule: "m"}); err != nil {
		t.Fatalf("Install v2: %v", err)
	}

	// Delete only the v1 install row, leaving v2 intact.
	if err := applier.DeleteInstall(ctx, store.RegistryInstallFilter{
		DefinitionKind:     store.DefinitionKindSearchParameter,
		TargetResourceType: "Patient",
		CanonicalURL:       "http://example.org/SearchParameter/patient-tag",
		Version:            "1.0.0",
	}); err != nil {
		t.Fatalf("DeleteInstall v1: %v", err)
	}

	remaining, err := installs.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Version != "2.0.0" {
		t.Fatalf("remaining installs = %+v, want one v2 row", remaining)
	}
}

func TestRegistryApplierDeleteDefinitionRemovesRecordAndTargets(t *testing.T) {
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	applier := modules.NewRegistryApplier(reg, defs, installs)
	ctx := context.Background()

	if err := applier.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := applier.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	if err := applier.DeleteDefinition(ctx, "http://hl7.org/fhir/StructureDefinition/Patient", "4.0.1"); err != nil {
		t.Fatalf("DeleteDefinition: %v", err)
	}
	if _, err := defs.Get(ctx, "http://hl7.org/fhir/StructureDefinition/Patient", "4.0.1"); err == nil {
		t.Fatal("expected Patient definition to be deleted")
	}
	// Rebuild snapshot should now fail because Patient is enabled but its SD is gone.
	if _, err := applier.RebuildSnapshot(ctx); err == nil {
		t.Fatal("expected snapshot rebuild to fail after deleting enabled SD")
	}
}

func TestRegistryApplierDisableResource(t *testing.T) {
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	applier := modules.NewRegistryApplier(reg, defs, installs)
	ctx := context.Background()

	if err := applier.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := applier.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	if err := applier.DisableResource(ctx, "Patient"); err != nil {
		t.Fatalf("DisableResource: %v", err)
	}
	snapshot, err := applier.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be disabled")
	}
}

func TestUninstallRemovesOnlyTargetModuleDefinitions(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "scheduling")); err != nil {
		t.Fatalf("Install scheduling: %v", err)
	}
	if err := mgr.Uninstall(ctx, "scheduling"); err != nil {
		t.Fatalf("Uninstall scheduling: %v", err)
	}

	core, err := mgr.Inspect(ctx, "core")
	if err != nil {
		t.Fatalf("Inspect core: %v", err)
	}
	for _, def := range core.Definitions {
		if def.CanonicalURL == "http://haistack.example.org/SearchParameter/Appointment-date-custom" {
			t.Fatal("scheduling definition should not remain after uninstall")
		}
	}
	if len(core.Definitions) != 1 {
		t.Fatalf("core definitions = %d, want 1", len(core.Definitions))
	}
}
