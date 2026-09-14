package terminology

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// EnableResult reports catalog opt-in outcomes.
type EnableResult struct {
	Count    int
	Warnings []string
}

// CatalogEnableOptions configures tenant opt-in to global terminology catalog entries.
type CatalogEnableOptions struct {
	Global      store.TerminologyStore
	Installs    store.TerminologyInstallStore
	Definitions store.DefinitionStore
}

// EnableCatalogEntry opts a tenant into one global CodeSystem or ValueSet.
func EnableCatalogEntry(ctx context.Context, opts CatalogEnableOptions, record store.TerminologyInstallRecord) (EnableResult, error) {
	if opts.Installs == nil {
		return EnableResult{}, fmt.Errorf("terminology install store is required")
	}
	if opts.Global == nil {
		return EnableResult{}, fmt.Errorf("global terminology store is required")
	}
	if record.CanonicalURL == "" {
		return EnableResult{}, fmt.Errorf("canonicalUrl is required")
	}
	if record.ResourceType == "" {
		record.ResourceType = "CodeSystem"
	}
	if !catalogResourceExists(ctx, opts.Global, record.ResourceType, record.CanonicalURL, record.Version) {
		return EnableResult{}, fmt.Errorf("terminology catalog entry %s|%s not found in global scope", record.CanonicalURL, record.Version)
	}
	if record.InstalledAt.IsZero() {
		record.InstalledAt = time.Now().UTC()
	}
	if err := opts.Installs.SetEnabled(ctx, record); err != nil {
		return EnableResult{}, err
	}
	return EnableResult{Count: 1}, nil
}

// EnableCatalogPack opts a tenant into all global CodeSystems and ValueSets from a package.
func EnableCatalogPack(ctx context.Context, opts CatalogEnableOptions, packName, packVersion string, enabled bool) (EnableResult, error) {
	if opts.Installs == nil {
		return EnableResult{}, fmt.Errorf("terminology install store is required")
	}
	if packName == "" {
		return EnableResult{}, fmt.Errorf("packName is required")
	}
	if opts.Global == nil {
		return EnableResult{}, fmt.Errorf("global terminology store is required")
	}
	if opts.Definitions == nil {
		return EnableResult{}, fmt.Errorf("definition store is required for pack-level enable")
	}
	defs, err := listPackTerminologyDefinitions(ctx, opts.Definitions, packName, packVersion)
	if err != nil {
		return EnableResult{}, err
	}
	result := EnableResult{}
	now := time.Now().UTC()
	for _, def := range defs {
		if !catalogResourceExists(ctx, opts.Global, def.FHIRResourceType, def.CanonicalURL, def.Version) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped %s|%s: not in global catalog", def.CanonicalURL, def.Version))
			continue
		}
		if err := opts.Installs.SetEnabled(ctx, store.TerminologyInstallRecord{
			PackName:     packName,
			PackVersion:  def.PackageVersion,
			ResourceType: def.FHIRResourceType,
			CanonicalURL: def.CanonicalURL,
			Version:      def.Version,
			Enabled:      enabled,
			InstalledAt:  now,
		}); err != nil {
			return result, err
		}
		result.Count++
	}
	if result.Count == 0 && len(result.Warnings) == 0 {
		return EnableResult{}, fmt.Errorf("no terminology resources found for pack %q", packName)
	}
	return result, nil
}

func catalogResourceExists(ctx context.Context, global store.TerminologyStore, typ, url, ver string) bool {
	if global == nil || url == "" {
		return false
	}
	if ver != "" {
		rec, err := global.FindResource(ctx, GlobalScopeID, typ, url, ver)
		return err == nil && rec != nil
	}
	resources, err := global.ListResources(ctx, GlobalScopeID, typ)
	if err != nil {
		return false
	}
	for _, rec := range resources {
		if rec.CanonicalURL == url {
			return true
		}
	}
	return false
}
