package registry

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// EnsureTerminologyPackEnabled opts the current tenant into all CodeSystems and
// ValueSets from an already-installed package version. Safe to call repeatedly.
func (m *Manager) EnsureTerminologyPackEnabled(ctx context.Context, packName, packVersion, sourceModule string) error {
	if m == nil || m.terminologyInstalls == nil || packName == "" || packVersion == "" {
		return nil
	}
	if m.definitions == nil {
		return nil
	}
	defs, err := m.definitions.List(ctx, store.DefinitionFilter{PackageName: packName})
	if err != nil {
		return err
	}
	now := m.now().UTC()
	for _, def := range defs {
		if def.PackageVersion != packVersion {
			continue
		}
		if def.FHIRResourceType != "CodeSystem" && def.FHIRResourceType != "ValueSet" {
			continue
		}
		termStore, termScope := m.terminologyTarget(def.FHIRResourceType)
		if termStore == nil {
			continue
		}
		rec, err := termStore.FindResource(ctx, termScope, def.FHIRResourceType, def.CanonicalURL, def.Version)
		if err != nil || rec == nil {
			continue
		}
		if err := m.terminologyInstalls.UpsertInstall(ctx, store.TerminologyInstallRecord{
			PackName:     packName,
			PackVersion:  packVersion,
			ResourceType: def.FHIRResourceType,
			CanonicalURL: def.CanonicalURL,
			Version:      def.Version,
			Enabled:      true,
			SourceModule: sourceModule,
			InstalledAt:  now,
		}); err != nil {
			return err
		}
	}
	return nil
}
