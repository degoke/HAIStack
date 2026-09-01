package conformance

import (
	"fmt"
	"path/filepath"

	"github.com/degoke/health-ai-stack/pkg/validate"
)

// DefaultIGValidatorConfig returns paths for IG example validation under repoRoot.
func DefaultIGValidatorConfig(repoRoot string) IGValidatorConfig {
	return IGValidatorConfig{
		BaseBundleRoot: filepath.Join(repoRoot, "pkg/registry/internal/bundles/r4"),
		IGResourcesDir: filepath.Join(repoRoot, "conformance/fsh-generated/resources"),
		ValidDir:       filepath.Join(repoRoot, "conformance/examples/valid"),
		InvalidDir:     filepath.Join(repoRoot, "conformance/examples/invalid"),
	}
}

func loadProfileCatalog(cfg IGValidatorConfig) (validate.MemoryProfileCatalog, error) {
	base, err := validate.LoadProfileCatalogFromDirTree(cfg.BaseBundleRoot)
	if err != nil {
		return nil, fmt.Errorf("load base profiles: %w", err)
	}
	ig, err := validate.LoadProfileCatalogFromDir(cfg.IGResourcesDir)
	if err != nil {
		return nil, fmt.Errorf("load IG profiles: %w", err)
	}
	return validate.MergeProfileCatalogs(base, ig), nil
}
