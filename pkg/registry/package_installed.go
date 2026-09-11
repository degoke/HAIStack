package registry

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// PackageVersionInstalled reports whether any definition from packageName@packageVersion
// is already present in the catalog.
func (m *Manager) PackageVersionInstalled(ctx context.Context, packageName, packageVersion string) (bool, error) {
	if m == nil || m.definitions == nil {
		return false, nil
	}
	if packageName == "" || packageVersion == "" {
		return false, nil
	}
	defs, err := m.definitions.List(ctx, store.DefinitionFilter{PackageName: packageName})
	if err != nil {
		return false, err
	}
	for _, def := range defs {
		if def.PackageVersion == packageVersion {
			return true, nil
		}
	}
	return false, nil
}
