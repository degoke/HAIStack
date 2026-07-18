package modules

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// RegistryApplier installs module-owned definitions, enables or disables base
// resources, and rebuilds the registry snapshot. It is the boundary between
// the module installer and the registry.Manager.
type RegistryApplier struct {
	registry *registry.Manager
	defs     store.DefinitionStore
	installs store.RegistryInstallStore
}

// NewRegistryApplier creates an applier backed by the registry manager and its
// persistence stores.
func NewRegistryApplier(reg *registry.Manager, defs store.DefinitionStore, installs store.RegistryInstallStore) *RegistryApplier {
	return &RegistryApplier{
		registry: reg,
		defs:     defs,
		installs: installs,
	}
}

// SeedBundled ensures the embedded FHIR base definitions are present.
func (a *RegistryApplier) SeedBundled(ctx context.Context) error {
	if err := a.registry.SeedBundled(ctx); err != nil {
		return fmt.Errorf("seed bundled definitions: %w", err)
	}
	return nil
}

// EnableResource marks a base resource type as enabled.
func (a *RegistryApplier) EnableResource(ctx context.Context, resourceType string) error {
	if err := a.registry.EnableResource(ctx, resourceType); err != nil {
		return fmt.Errorf("enable resource %q: %w", resourceType, err)
	}
	return nil
}

// DisableResource marks a base resource type as disabled.
func (a *RegistryApplier) DisableResource(ctx context.Context, resourceType string) error {
	if err := a.registry.DisableResource(ctx, resourceType); err != nil {
		return fmt.Errorf("disable resource %q: %w", resourceType, err)
	}
	return nil
}

// InstallDefinition ingests a definition JSON with module provenance.
func (a *RegistryApplier) InstallDefinition(ctx context.Context, jsonData []byte, provenance registry.InstallProvenance) error {
	if err := a.registry.InstallDefinition(ctx, jsonData, provenance); err != nil {
		return fmt.Errorf("install definition: %w", err)
	}
	return nil
}

// DeleteDefinition removes a definition catalog entry and its target mappings.
func (a *RegistryApplier) DeleteDefinition(ctx context.Context, canonicalURL, version string) error {
	if err := a.registry.DeleteDefinition(ctx, canonicalURL, version); err != nil {
		return fmt.Errorf("delete definition %s: %w", canonicalURL, err)
	}
	return nil
}

// DeleteInstall removes registry install rows matching the filter.
func (a *RegistryApplier) DeleteInstall(ctx context.Context, filter store.RegistryInstallFilter) error {
	if err := a.installs.Delete(ctx, filter); err != nil {
		return fmt.Errorf("delete registry install: %w", err)
	}
	return nil
}

// RebuildSnapshot compiles the registry snapshot.
func (a *RegistryApplier) RebuildSnapshot(ctx context.Context) (*registry.Snapshot, error) {
	snapshot, err := a.registry.RebuildSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebuild snapshot: %w", err)
	}
	return snapshot, nil
}

// DefinitionStore returns the underlying definition store for queries.
func (a *RegistryApplier) DefinitionStore() store.DefinitionStore {
	return a.defs
}

// InstallStore returns the underlying registry install store for queries.
func (a *RegistryApplier) InstallStore() store.RegistryInstallStore {
	return a.installs
}
