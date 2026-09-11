package registry

import (
	"context"
)

// PackageVersionInstalled reports whether packageName@packageVersion finished
// install successfully (CompletePackageInstall was called). When PackageInstalls
// is not wired (unit tests only), this always returns false so installs proceed.
func (m *Manager) PackageVersionInstalled(ctx context.Context, packageName, packageVersion string) (bool, error) {
	if m == nil || m.packageInstalls == nil {
		return false, nil
	}
	if packageName == "" || packageVersion == "" {
		return false, nil
	}
	return m.packageInstalls.IsComplete(ctx, packageName, packageVersion)
}
