package modules

import (
	"context"
	"fmt"
	"sort"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// CapabilitySnapshotBuilder returns a module-centric view of what each
// installed module contributed, built from ModuleStore plus
// RegistryInstallStore.
type CapabilitySnapshotBuilder struct {
	modules  store.ModuleStore
	installs store.RegistryInstallStore
}

// NewCapabilitySnapshotBuilder creates a builder backed by the module and
// registry install stores.
func NewCapabilitySnapshotBuilder(modules store.ModuleStore, installs store.RegistryInstallStore) *CapabilitySnapshotBuilder {
	return &CapabilitySnapshotBuilder{
		modules:  modules,
		installs: installs,
	}
}

// Build returns one InstalledModule per registered module, with resources and
// definitions derived from registry install rows that name the module as their
// source.
func (b *CapabilitySnapshotBuilder) Build(ctx context.Context) ([]InstalledModule, error) {
	records, err := b.modules.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}

	allInstalls, err := b.installs.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		return nil, fmt.Errorf("list registry installs: %w", err)
	}
	installsByModule := make(map[string][]store.RegistryInstallRecord)
	for _, inst := range allInstalls {
		installsByModule[inst.SourceModule] = append(installsByModule[inst.SourceModule], inst)
	}

	out := make([]InstalledModule, 0, len(records))
	for _, rec := range records {
		manifest, err := manifestFromMetadata(rec.Metadata)
		if err != nil {
			return nil, fmt.Errorf("decode manifest for %q: %w", rec.Name, err)
		}
		mod := InstalledModule{
			Name:         rec.Name,
			Version:      rec.Version,
			Dependencies: append([]DependencyRef(nil), manifest.Dependencies...),
			Resources:    sortedStringSet(manifest.Resources),
			Deferred:     declarationsFromManifest(manifest),
			RegisteredAt: rec.RegisteredAt,
		}
		for _, inst := range installsByModule[rec.Name] {
			if inst.DefinitionKind == store.DefinitionKindStructureDefinition {
				continue
			}
			mod.Definitions = append(mod.Definitions, DefinitionRef{
				CanonicalURL: inst.CanonicalURL,
				Version:      inst.Version,
			})
		}
		sort.Slice(mod.Definitions, func(i, j int) bool {
			if mod.Definitions[i].CanonicalURL != mod.Definitions[j].CanonicalURL {
				return mod.Definitions[i].CanonicalURL < mod.Definitions[j].CanonicalURL
			}
			return mod.Definitions[i].Version < mod.Definitions[j].Version
		})
		out = append(out, mod)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
