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
	return terminology.EnsureCatalogPackOptIn(ctx, terminology.CatalogEnableOptions{
		Global:      m.globalTerminology,
		Installs:    m.terminologyInstalls,
		Definitions: m.definitions,
	}, packName, packVersion, sourceModule)
}
