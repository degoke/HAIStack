package terminology

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// EnsureInstallOptIn records tenant opt-in when absent. It preserves explicit
// opt-out (enabled=false) and existing installed_at timestamps.
func EnsureInstallOptIn(ctx context.Context, installs store.TerminologyInstallStore, record store.TerminologyInstallRecord) error {
	if installs == nil {
		return nil
	}
	if record.CanonicalURL == "" || record.ResourceType == "" {
		return nil
	}
	existing, err := installs.ListInstalled(ctx, store.TerminologyInstallFilter{
		ResourceType: record.ResourceType,
		CanonicalURL: record.CanonicalURL,
		Version:      record.Version,
	})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if record.InstalledAt.IsZero() {
		record.InstalledAt = time.Now().UTC()
	}
	return installs.UpsertInstall(ctx, record)
}

// EnsureCatalogPackOptIn opts the tenant into package terminology entries that
// are absent. It never overrides explicit opt-out or refreshes installed_at.
func EnsureCatalogPackOptIn(ctx context.Context, opts CatalogEnableOptions, packName, packVersion, sourceModule string) error {
	if opts.Installs == nil || packName == "" || packVersion == "" {
		return nil
	}
	if opts.Global == nil || opts.Definitions == nil {
		return nil
	}
	defs, err := listPackTerminologyDefinitions(ctx, opts.Definitions, packName, packVersion)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if !catalogResourceExists(ctx, opts.Global, def.FHIRResourceType, def.CanonicalURL, def.Version) {
			continue
		}
		if err := EnsureInstallOptIn(ctx, opts.Installs, store.TerminologyInstallRecord{
			PackName:     packName,
			PackVersion:  packVersion,
			ResourceType: def.FHIRResourceType,
			CanonicalURL: def.CanonicalURL,
			Version:      def.Version,
			Enabled:      true,
			SourceModule: sourceModule,
		}); err != nil {
			return err
		}
	}
	return nil
}

func listPackTerminologyDefinitions(ctx context.Context, definitions store.DefinitionStore, packName, packVersion string) ([]store.DefinitionResourceRecord, error) {
	if definitions == nil {
		return nil, fmt.Errorf("definition store is required")
	}
	if packName == "" {
		return nil, fmt.Errorf("packName is required")
	}
	defs, err := definitions.List(ctx, store.DefinitionFilter{PackageName: packName})
	if err != nil {
		return nil, err
	}
	var out []store.DefinitionResourceRecord
	for _, def := range defs {
		if packVersion != "" && def.PackageVersion != packVersion {
			continue
		}
		if def.FHIRResourceType != "CodeSystem" && def.FHIRResourceType != "ValueSet" {
			continue
		}
		out = append(out, def)
	}
	return out, nil
}
