package ai

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/validate"
)

type phiPathBundle struct {
	segment    []string
	segmentIdx *pathIndex
	catalogIdx *pathIndex
	labels     []string
}

type fhirPathPHIIndex struct {
	mu          sync.RWMutex
	engine      fhirpath.Engine
	profiles    validate.ProfileCatalog
	rules       PHIStructureRules
	catalog     *PHICatalog
	mode        PHIMode
	evalMode    EvalMode
	base        map[string]phiPathBundle
	byProfile   map[string]phiPathBundle
	merged      map[string]phiPathBundle
}

func newFHIRPathPHIIndex(engine fhirpath.Engine, profiles validate.ProfileCatalog, rules PHIStructureRules, catalog *PHICatalog, mode PHIMode, evalMode EvalMode) *fhirPathPHIIndex {
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	if mode == "" {
		mode = PHIModeStandard
	}
	if evalMode == "" {
		evalMode = EvalModeKeywordsOnly
	}
	rules = StructureRulesForMode(mode, rules)
	return &fhirPathPHIIndex{
		engine:    engine,
		profiles:  profiles,
		rules:     rules,
		catalog:   catalog,
		mode:      mode,
		evalMode:  evalMode,
		base:      make(map[string]phiPathBundle),
		byProfile: make(map[string]phiPathBundle),
		merged:    make(map[string]phiPathBundle),
	}
}

func (idx *fhirPathPHIIndex) bundleFor(ctx context.Context, resourceType string, resource map[string]any) (phiPathBundle, error) {
	if err := ctx.Err(); err != nil {
		return phiPathBundle{}, err
	}
	profiles := metaProfileURLs(resource)
	cacheKey := mergedBundleCacheKey(resourceType, profiles)
	idx.mu.RLock()
	if b, ok := idx.merged[cacheKey]; ok {
		idx.mu.RUnlock()
		return b, nil
	}
	idx.mu.RUnlock()

	base, err := idx.baseBundle(ctx, resourceType)
	if err != nil {
		return phiPathBundle{}, err
	}
	merged := base
	for _, url := range profiles {
		profileBundle, err := idx.profileBundle(ctx, url, resourceType)
		if err != nil {
			return phiPathBundle{}, err
		}
		merged = mergePathBundles(merged, profileBundle)
	}
	merged = finalizePathBundle(merged, idx.catalog, resourceType)

	idx.mu.Lock()
	idx.merged[cacheKey] = merged
	idx.mu.Unlock()
	return merged, nil
}

func mergedBundleCacheKey(resourceType string, profiles []string) string {
	if len(profiles) == 0 {
		return resourceType
	}
	sorted := append([]string(nil), profiles...)
	sort.Strings(sorted)
	return resourceType + "|" + strings.Join(sorted, ",")
}

func (idx *fhirPathPHIIndex) baseBundle(ctx context.Context, resourceType string) (phiPathBundle, error) {
	idx.mu.RLock()
	if b, ok := idx.base[resourceType]; ok {
		idx.mu.RUnlock()
		return b, nil
	}
	idx.mu.RUnlock()

	paths, err := idx.buildBasePaths(ctx, resourceType)
	if err != nil {
		return phiPathBundle{}, err
	}
	bundle := idx.compileBundle(paths, resourceType)

	idx.mu.Lock()
	idx.base[resourceType] = bundle
	idx.mu.Unlock()
	return bundle, nil
}

func (idx *fhirPathPHIIndex) profileBundle(ctx context.Context, profileURL, resourceType string) (phiPathBundle, error) {
	cacheKey := profileURL + "|" + resourceType
	idx.mu.RLock()
	if b, ok := idx.byProfile[cacheKey]; ok {
		idx.mu.RUnlock()
		return b, nil
	}
	idx.mu.RUnlock()

	if idx.profiles == nil {
		return phiPathBundle{}, nil
	}
	sd, err := lookupStructureDefinition(idx.profiles, profileURL)
	if err != nil {
		return phiPathBundle{}, err
	}
	if sd == nil || sd.Type != resourceType {
		return phiPathBundle{}, nil
	}
	paths := sensitiveFHIRPathsFromStructureDefinition(sd, idx.rules)
	bundle := idx.compileBundle(paths, resourceType)

	idx.mu.Lock()
	idx.byProfile[cacheKey] = bundle
	idx.mu.Unlock()
	return bundle, nil
}

func (idx *fhirPathPHIIndex) buildBasePaths(ctx context.Context, resourceType string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(expr string) {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return
		}
		if _, ok := seen[expr]; ok {
			return
		}
		seen[expr] = struct{}{}
		out = append(out, expr)
	}

	if idx.catalog != nil {
		for _, suffix := range idx.catalog.globalPathSuffixes() {
			add(suffix)
		}
		for _, suffix := range idx.catalog.ResourcePathSuffixes[resourceType] {
			add(suffix)
		}
		for _, el := range idx.catalog.ElementsForResource(resourceType) {
			add(el)
		}
	}

	if idx.mode != PHIModeCatalog && idx.profiles != nil {
		sd, err := lookupBaseStructureDefinition(idx.profiles, resourceType)
		if err != nil {
			return nil, err
		}
		if sd != nil {
			for _, expr := range sensitiveFHIRPathsFromStructureDefinition(sd, idx.rules) {
				add(expr)
			}
		}
	}
	return out, nil
}

func (idx *fhirPathPHIIndex) compileBundle(paths []string, resourceType string) phiPathBundle {
	segment, compileExprs := ExpandFHIRPathExpressions(paths)
	segmentSet := newPathIndex(segment)
	evalMode := idx.evalMode
	var labels []string
	if evalMode != EvalModeNever {
		for _, expr := range compileExprs {
			if evalMode == EvalModeKeywordsOnly {
				seg := SegmentPathFromFHIRPathExpr(expr)
				if seg != "" && segmentSet != nil && segmentSet.Match(seg) {
					continue
				}
			}
			labels = append(labels, expr)
		}
	}
	return phiPathBundle{
		segment:    segment,
		segmentIdx: segmentSet,
		catalogIdx: catalogPathIndex(idx.catalog, resourceType),
		labels:     labels,
	}
}

func finalizePathBundle(b phiPathBundle, catalog *PHICatalog, resourceType string) phiPathBundle {
	b.segmentIdx = mergePathIndices(b.segmentIdx, newPathIndex(b.segment))
	b.catalogIdx = catalogPathIndex(catalog, resourceType)
	return b
}

func mergePathBundles(a, b phiPathBundle) phiPathBundle {
	segSeen := make(map[string]struct{})
	var segment []string
	for _, s := range append(a.segment, b.segment...) {
		if _, ok := segSeen[s]; ok {
			continue
		}
		segSeen[s] = struct{}{}
		segment = append(segment, s)
	}
	labels := append([]string(nil), a.labels...)
	seenExpr := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		seenExpr[l] = struct{}{}
	}
	for _, l := range b.labels {
		if l == "" {
			continue
		}
		if _, ok := seenExpr[l]; ok {
			continue
		}
		seenExpr[l] = struct{}{}
		labels = append(labels, l)
	}
	return phiPathBundle{
		segment:    segment,
		segmentIdx: mergePathIndices(a.segmentIdx, b.segmentIdx, newPathIndex(segment)),
		catalogIdx: a.catalogIdx,
		labels:     labels,
	}
}

func lookupBaseStructureDefinition(catalog validate.ProfileCatalog, resourceType string) (*validate.StructureDefinition, error) {
	return lookupStructureDefinition(catalog, validate.BaseStructureDefinitionURL(resourceType))
}

func lookupStructureDefinition(catalog validate.ProfileCatalog, canonicalURL string) (*validate.StructureDefinition, error) {
	if resolver, ok := catalog.(validate.ProfileCatalogResolver); ok {
		sd, err := resolver.ResolveStructureDefinition(canonicalURL)
		if err != nil {
			if err == validate.ErrProfileNotFound {
				return nil, nil
			}
			return nil, err
		}
		return sd, nil
	}
	sd, ok := catalog.GetStructureDefinition(canonicalURL)
	if !ok {
		return nil, nil
	}
	return sd, nil
}
