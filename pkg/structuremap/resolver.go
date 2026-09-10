package structuremap

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Resolver loads StructureMap resources by canonical URL.
type Resolver interface {
	Resolve(ctx context.Context, canonical string) (Map, error)
}

// StoreResolver resolves StructureMap resources from a ResourceStore and,
// when available, a DefinitionStore fallback.
type StoreResolver struct {
	Resources store.ResourceStore
	Registry  store.DefinitionStore
}

func (r StoreResolver) Resolve(ctx context.Context, canonical string) (Map, error) {
	if canonical == "" {
		return Map{}, fmt.Errorf("StructureMap canonical URL is required")
	}
	if r.Resources != nil {
		ids, err := r.Resources.ListIDs(ctx, "StructureMap", 10000, 0)
		if err != nil {
			return Map{}, fmt.Errorf("list StructureMap resources: %w", err)
		}
		for _, id := range ids {
			env, err := r.Resources.Read(ctx, "StructureMap", id)
			if err != nil {
				continue
			}
			m, err := ParseMap(env.JSON)
			if err != nil {
				continue
			}
			if matchesCanonical(m, canonical) {
				return m, nil
			}
		}
	}
	if r.Registry != nil {
		url, version := splitCanonical(canonical)
		record, err := r.Registry.Get(ctx, url, version)
		if err == nil && record != nil && len(record.JSONData) > 0 {
			m, err := ParseMap(record.JSONData)
			if err != nil {
				return Map{}, fmt.Errorf("parse StructureMap %s: %w", canonical, err)
			}
			if matchesCanonical(m, canonical) {
				return m, nil
			}
		}
	}
	return Map{}, fmt.Errorf("StructureMap not found: %s", canonical)
}

// StaticResolver resolves maps from an in-memory table.
type StaticResolver map[string]Map

func (r StaticResolver) Resolve(_ context.Context, canonical string) (Map, error) {
	if m, ok := r[canonical]; ok {
		return m, nil
	}
	url, version := splitCanonical(canonical)
	for _, m := range r {
		if matchesCanonical(m, url) || (version != "" && matchesCanonical(m, canonical)) {
			return m, nil
		}
	}
	return Map{}, fmt.Errorf("StructureMap not found: %s", canonical)
}

func matchesCanonical(m Map, canonical string) bool {
	if canonical == "" {
		return false
	}
	if Canonical(m) == canonical || m.URL == canonical {
		return true
	}
	url, version := splitCanonical(canonical)
	if m.URL == url && (version == "" || m.Version == version) {
		return true
	}
	return false
}

func splitCanonical(canonical string) (url, version string) {
	parts := strings.SplitN(canonical, "|", 2)
	url = parts[0]
	if len(parts) == 2 {
		version = parts[1]
	}
	return url, version
}

// ResolveEnvelope parses a StructureMap envelope.
func ResolveEnvelope(env *types.ResourceEnvelope) (Map, error) {
	if env == nil {
		return Map{}, fmt.Errorf("StructureMap envelope is nil")
	}
	return ParseMap(env.JSON)
}
