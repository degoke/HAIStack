package registry

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/terminology"
)

// EnsureTerminologyPackEnabled opts the current tenant into all CodeSystems and
// ValueSets from an already-installed package version. Safe to call repeatedly;
// explicit tenant opt-out is preserved.
func (m *Manager) EnsureTerminologyPackEnabled(ctx context.Context, packName, packVersion, sourceModule string) error {
	if m == nil {
		return nil
	}
	installs := TerminologyInstallsFromContext(ctx)
	if installs == nil {
		installs = m.terminologyInstalls
	}
	return terminology.EnsureCatalogPackOptIn(ctx, terminology.CatalogEnableOptions{
		Global:      m.globalTerminology,
		Installs:    installs,
		Definitions: m.definitions,
	}, packName, packVersion, sourceModule)
}
