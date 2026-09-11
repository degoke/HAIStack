package packages

import (
	"context"
	"fmt"
)

// InstallConfigured installs each spec idempotently. Already-installed package
// versions are skipped without error.
func InstallConfigured(ctx context.Context, installer *Installer, specs []InstallSpec) error {
	if installer == nil {
		return fmt.Errorf("package installer is not configured")
	}
	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return err
		}
		_, _, err := installer.InstallIfNeeded(ctx, spec)
		if err != nil {
			label := spec.Normalize().PackageID
			if label == "" {
				label = spec.Path
			}
			return fmt.Errorf("install package %s: %w", label, err)
		}
	}
	return nil
}

// InstallIfNeeded installs one package when that version is not already complete.
// The returned skipped flag is true when CompletePackageInstall already recorded the version.
func (i *Installer) InstallIfNeeded(ctx context.Context, spec InstallSpec) (*InstallResult, bool, error) {
	if i == nil || i.Registry == nil {
		return nil, false, fmt.Errorf("package installer is not configured")
	}
	spec = spec.Normalize()
	if err := spec.Validate(); err != nil {
		return nil, false, err
	}
	installed, err := i.Registry.PackageVersionInstalled(ctx, spec.PackageID, spec.Version)
	if err != nil {
		return nil, false, err
	}
	if installed {
		sourceModule := spec.PackageID
		if err := i.Registry.EnsureTerminologyPackEnabled(ctx, spec.PackageID, spec.Version, sourceModule); err != nil {
			return nil, true, err
		}
		return &InstallResult{PackageID: spec.PackageID, Version: spec.Version}, true, nil
	}
	if spec.Path != "" {
		result, err := i.InstallFromDirectoryVersion(ctx, spec.Path, spec.PackageID, spec.Version)
		return result, false, err
	}
	result, err := i.InstallFromRegistry(ctx, spec.PackageID, spec.Version)
	return result, false, err
}
