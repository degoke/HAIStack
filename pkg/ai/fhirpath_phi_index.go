package ai

import (
	"context"
	"strings"
	"sync"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/validate"
)

type phiPathBundle struct {
	segment  []string
	compiled []fhirpath.CompiledExpression
	labels   []string
}

type fhirPathPHIIndex struct {
	mu       sync.RWMutex
	engine   fhirpath.Engine
	profiles validate.ProfileCatalog
	rules    PHIStructureRules
	catalog  *PHICatalog
	base     map[string]phiPathBundle
	byProfile map[string]phiPathBundle
}

func newFHIRPathPHIIndex(engine fhirpath.Engine, profiles validate.ProfileCatalog, rules PHIStructureRules, catalog *PHICatalog) *fhirPathPHIIndex {
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	return &fhirPathPHIIndex{
		engine:    engine,
		profiles:  profiles,
		rules:     rules,
		catalog:   catalog,
		base:      make(map[string]phiPathBundle),
		byProfile: make(map[string]phiPathBundle),
	}
}

func (idx *fhirPathPHIIndex) bundleFor(ctx context.Context, resourceType string, resource map[string]any) (phiPathBundle, error) {
	if err := ctx.Err(); err != nil {
		return phiPathBundle{}, err
	}
	base, err := idx.baseBundle(ctx, resourceType)
	if err != nil {
		return phiPathBundle{}, err
	}
	merged := base
	for _, url := range metaProfileURLs(resource) {
		profileBundle, err := idx.profileBundle(ctx, url, resourceType)
		if err != nil {
			return phiPathBundle{}, err
		}
		merged = mergePathBundles(merged, profileBundle)
	}
	return merged, nil
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
	bundle := idx.compileBundle(paths)

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
	bundle := idx.compileBundle(paths)

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

	if idx.profiles != nil {
		sd, err := lookupBaseStructureDefinition(idx.profiles, resourceType)
		if err != nil {
			return nil, err
		}
		if sd != nil {
			for _, expr := range SensitiveFHIRPathsFromStructureDefinition(sd, idx.rules, idx.catalog) {
				add(expr)
			}
		}
	}
	return out, nil
}

func (idx *fhirPathPHIIndex) compileBundle(paths []string) phiPathBundle {
	segment, compileExprs := ExpandFHIRPathExpressions(paths)
	var compiled []fhirpath.CompiledExpression
	var labels []string
	if idx.engine != nil {
		for _, expr := range compileExprs {
			c, err := idx.engine.Compile(expr)
			if err != nil {
				continue
			}
			compiled = append(compiled, c)
			labels = append(labels, expr)
		}
	}
	return phiPathBundle{
		segment:  segment,
		compiled: compiled,
		labels:   labels,
	}
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
	compiled := append([]fhirpath.CompiledExpression(nil), a.compiled...)
	labels := append([]string(nil), a.labels...)
	seenExpr := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		seenExpr[l] = struct{}{}
	}
	for i, c := range b.compiled {
		l := b.labels[i]
		if l == "" {
			l = c.Expr()
		}
		if _, ok := seenExpr[l]; ok {
			continue
		}
		seenExpr[l] = struct{}{}
		compiled = append(compiled, c)
		labels = append(labels, l)
	}
	return phiPathBundle{segment: segment, compiled: compiled, labels: labels}
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
