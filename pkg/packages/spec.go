package packages

import (
	"fmt"
	"path/filepath"
	"strings"
)

// InstallSpec declares one package to install at server startup or programmatically.
type InstallSpec struct {
	PackageID string
	Version   string
	Path      string // local directory; when set, installs from disk instead of the registry
}

// Normalize fills derived fields and returns a copy safe to use.
func (s InstallSpec) Normalize() InstallSpec {
	out := s
	out.PackageID = strings.TrimSpace(out.PackageID)
	out.Version = strings.TrimSpace(out.Version)
	out.Path = strings.TrimSpace(out.Path)
	if out.Path != "" {
		if abs, err := filepath.Abs(out.Path); err == nil {
			out.Path = abs
		}
		if out.PackageID == "" {
			out.PackageID = filepath.Base(out.Path)
		}
	}
	return out
}

// Validate checks required fields for one install spec.
func (s InstallSpec) Validate() error {
	norm := s.Normalize()
	if norm.Path != "" {
		if norm.Version == "" {
			return fmt.Errorf("local package path %q requires version", norm.Path)
		}
		return nil
	}
	if norm.PackageID == "" || norm.Version == "" {
		return fmt.Errorf("registry package install requires packageId and version")
	}
	return nil
}
