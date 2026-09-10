package conceptmap

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Resolver loads ConceptMap resources by canonical URL.
type Resolver interface {
	Resolve(ctx context.Context, canonical string) (Map, error)
}

// StoreResolver resolves ConceptMaps from ResourceStore with DefinitionStore fallback.
type StoreResolver struct {
	Resources store.ResourceStore
	Registry  store.DefinitionStore
	mu        sync.Mutex
}

func (r *StoreResolver) Resolve(ctx context.Context, canonical string) (Map, error) {
	if canonical == "" {
		return Map{}, fmt.Errorf("ConceptMap canonical URL is required")
	}
	if r.Resources != nil {
		r.mu.Lock()
		byURL, err := r.buildIndex(ctx)
		r.mu.Unlock()
		if err != nil {
			return Map{}, err
		}
		if m, ok, resolveErr := resolveFromIndex(byURL, canonical); resolveErr != nil {
			return Map{}, resolveErr
		} else if ok {
			return m, nil
		}
	}
	if r.Registry != nil {
		url, version := splitCanonical(canonical)
		record, err := r.Registry.Get(ctx, url, version)
		if err == nil && record != nil && len(record.JSONData) > 0 {
			m, err := ParseMap(record.JSONData)
			if err != nil {
				return Map{}, fmt.Errorf("parse ConceptMap %s: %w", canonical, err)
			}
			if version != "" {
				if matchesCanonical(m, canonical) {
					return m, nil
				}
			} else if m.URL == url {
				return m, nil
			}
		}
	}
	return Map{}, ErrNotFound(canonical)
}

func (r *StoreResolver) buildIndex(ctx context.Context) (map[string][]Map, error) {
	byURL := map[string][]Map{}
	if r.Resources == nil {
		return byURL, nil
	}
	ids, err := r.Resources.ListIDs(ctx, "ConceptMap", 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("list ConceptMap resources: %w", err)
	}
	for _, id := range ids {
		env, err := r.Resources.Read(ctx, "ConceptMap", id)
		if err != nil {
			continue
		}
		m, err := ParseMap(env.JSON)
		if err != nil || m.URL == "" {
			continue
		}
		byURL[m.URL] = append(byURL[m.URL], m)
	}
	return byURL, nil
}

// StaticResolver resolves maps from an in-memory table.
type StaticResolver map[string]Map

func (r StaticResolver) Resolve(_ context.Context, canonical string) (Map, error) {
	if m, ok := r[canonical]; ok {
		return m, nil
	}
	byURL := map[string][]Map{}
	keys := make([]string, 0, len(r))
	for key := range r {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		m := r[key]
		if m.URL != "" {
			byURL[m.URL] = append(byURL[m.URL], m)
		}
	}
	if m, ok, err := resolveFromIndex(byURL, canonical); err != nil {
		return Map{}, err
	} else if ok {
		return m, nil
	}
	return Map{}, ErrNotFound(canonical)
}

func resolveFromIndex(byURL map[string][]Map, canonical string) (Map, bool, error) {
	if m, ok := lookupExact(byURL, canonical); ok {
		return m, true, nil
	}
	url, version := splitCanonical(canonical)
	candidates := byURL[url]
	if len(candidates) == 0 {
		return Map{}, false, nil
	}
	if version != "" {
		var matches []Map
		for _, m := range candidates {
			if matchesCanonical(m, canonical) {
				matches = append(matches, m)
			}
		}
		if len(matches) == 0 {
			return Map{}, false, nil
		}
		if len(matches) > 1 {
			return Map{}, false, fmt.Errorf("ambiguous ConceptMap canonical %s", canonical)
		}
		return matches[0], true, nil
	}
	latest, err := latestMapVersion(candidates)
	if err != nil {
		return Map{}, false, err
	}
	return latest, true, nil
}

func lookupExact(byURL map[string][]Map, canonical string) (Map, bool) {
	url, version := splitCanonical(canonical)
	if version == "" {
		return Map{}, false
	}
	for _, m := range byURL[url] {
		if Canonical(m) == canonical {
			return m, true
		}
	}
	return Map{}, false
}

func matchesCanonical(m Map, canonical string) bool {
	if canonical == "" {
		return false
	}
	if Canonical(m) == canonical || m.URL == canonical {
		return true
	}
	url, version := splitCanonical(canonical)
	return m.URL == url && version != "" && m.Version == version
}

// ResolveEnvelope parses a ConceptMap envelope.
func ResolveEnvelope(env *types.ResourceEnvelope) (Map, error) {
	if env == nil {
		return Map{}, fmt.Errorf("ConceptMap envelope is nil")
	}
	return ParseMap(env.JSON)
}
