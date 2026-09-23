package ai

import (
	"context"
	"strings"
	"sync"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/validate"
)

type fhirPathPHIIndex struct {
	mu       sync.RWMutex
	engine   fhirpath.Engine
	profiles validate.ProfileCatalog
	rules    PHIStructureRules
	catalog  *PHICatalog
	byType   map[string][]string
	compiled map[string][]fhirpath.CompiledExpression
}

func newFHIRPathPHIIndex(engine fhirpath.Engine, profiles validate.ProfileCatalog, rules PHIStructureRules, catalog *PHICatalog) *fhirPathPHIIndex {
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	return &fhirPathPHIIndex{
		engine:   engine,
		profiles: profiles,
		rules:    rules,
		catalog:  catalog,
		byType:   make(map[string][]string),
		compiled: make(map[string][]fhirpath.CompiledExpression),
	}
}

func (idx *fhirPathPHIIndex) paths(ctx context.Context, resourceType string) ([]string, error) {
	idx.mu.RLock()
	if paths, ok := idx.byType[resourceType]; ok {
		idx.mu.RUnlock()
		return paths, nil
	}
	idx.mu.RUnlock()

	paths, err := idx.buildPaths(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	idx.mu.Lock()
	idx.byType[resourceType] = paths
	if idx.engine != nil {
		var compiled []fhirpath.CompiledExpression
		for _, expr := range paths {
			c, err := idx.engine.Compile(expr)
			if err != nil {
				// Segment redaction still applies; skip expressions the compiler rejects
				// (for example child nodes whose names collide with FHIRPath keywords).
				continue
			}
			compiled = append(compiled, c)
		}
		idx.compiled[resourceType] = compiled
	}
	idx.mu.Unlock()
	return paths, nil
}

func (idx *fhirPathPHIIndex) buildPaths(ctx context.Context, resourceType string) ([]string, error) {
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

func lookupBaseStructureDefinition(catalog validate.ProfileCatalog, resourceType string) (*validate.StructureDefinition, error) {
	url := validate.BaseStructureDefinitionURL(resourceType)
	if resolver, ok := catalog.(validate.ProfileCatalogResolver); ok {
		return resolver.ResolveStructureDefinition(url)
	}
	sd, ok := catalog.GetStructureDefinition(url)
	if !ok {
		return nil, nil
	}
	return sd, nil
}
