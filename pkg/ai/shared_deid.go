package ai

import (
	"context"
	"fmt"
	"sync"
)

var sharedDeidentifierMu sync.Mutex
var sharedDeidentifiers = map[string]*FHIRDeidentifier{}

// SharedFHIRDeidentifier returns a process-wide FHIRDeidentifier for equivalent
// configuration. Use when multiple Executors should share warm path caches.
func SharedFHIRDeidentifier(cfg FHIRDeidentifierConfig) (*FHIRDeidentifier, error) {
	key := sharedDeidentifierKey(cfg)
	sharedDeidentifierMu.Lock()
	defer sharedDeidentifierMu.Unlock()
	if d, ok := sharedDeidentifiers[key]; ok {
		return d, nil
	}
	cfg.UseShared = false
	d, err := newFHIRDeidentifierWithConfig(cfg)
	if err != nil {
		return nil, err
	}
	sharedDeidentifiers[key] = d
	return d, nil
}

func sharedDeidentifierKey(cfg FHIRDeidentifierConfig) string {
	mode := cfg.Mode
	if mode == "" {
		mode = PHIModeStandard
	}
	eval := cfg.EvalMode
	if eval == "" {
		eval = EvalModeKeywordsOnly
	}
	redacted := cfg.Redacted
	if redacted == "" {
		redacted = DefaultRedactedValue
	}
	catalog := canonicalCatalogKey(cfg.Catalog)
	profiles := canonicalProfileCatalogKey(cfg.Profiles)
	rules := canonicalRulesKey(cfg.Rules)
	engine := canonicalEngineKey(cfg.Engine)
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", mode, eval, catalog, profiles, rules, engine, redacted)
}

// WarmPathIndex pre-builds path bundles for resource types.
func WarmPathIndex(ctx context.Context, d *FHIRDeidentifier, resourceTypes ...string) error {
	if d == nil || d.index == nil {
		return nil
	}
	if len(resourceTypes) == 0 {
		resourceTypes = DefaultWarmResourceTypes
	}
	for _, rt := range resourceTypes {
		if rt == "" {
			continue
		}
		if _, err := d.index.baseBundle(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}
