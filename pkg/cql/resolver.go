package cql

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// StoreLibraryResolver loads FHIR Library resources from a ResourceStore and
// optionally a DefinitionStore, then compiles their CQL content.
type StoreLibraryResolver struct {
	Resources store.ResourceStore
	Registry  store.DefinitionStore
	Engine    *Engine
	mu        sync.Mutex
}

func (r *StoreLibraryResolver) Resolve(ctx context.Context, canonical string) (*Library, error) {
	if canonical == "" {
		return nil, errf("%w: library canonical URL is required", ErrLibraryNotFound)
	}
	if r.Engine == nil {
		return nil, ErrEngineUnavailable
	}
	if r.Resources != nil {
		r.mu.Lock()
		byURL, err := r.buildIndex(ctx)
		r.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if lib, ok, resolveErr := resolveLibraryIndex(byURL, canonical); resolveErr != nil {
			return nil, resolveErr
		} else if ok {
			return lib, nil
		}
	}
	if r.Registry != nil {
		url, version := splitCanonical(canonical)
		record, err := r.Registry.Get(ctx, url, version)
		if err == nil && record != nil && len(record.JSONData) > 0 {
			src, libURL, name, ver, err := parseLibraryJSON(record.JSONData)
			if err != nil {
				return nil, err
			}
			lib, err := compileLibrarySource(r.Engine, src, libURL, name, ver)
			if err != nil {
				return nil, err
			}
			if matchesCanonical(lib, canonical) {
				return lib, nil
			}
		}
	}
	return nil, errf("%w: %s", ErrLibraryNotFound, canonical)
}

func (r *StoreLibraryResolver) buildIndex(ctx context.Context) (map[string][]*Library, error) {
	byURL := map[string][]*Library{}
	if r.Resources == nil {
		return byURL, nil
	}
	ids, err := r.Resources.ListIDs(ctx, "Library", 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("list Library resources: %w", err)
	}
	for _, id := range ids {
		env, err := r.Resources.Read(ctx, "Library", id)
		if err != nil || env == nil {
			continue
		}
		lib, err := compileEnvelope(r.Engine, env)
		if err != nil || lib == nil {
			continue
		}
		key := lib.URL
		if key == "" {
			key = lib.Name
		}
		if key == "" {
			continue
		}
		byURL[key] = append(byURL[key], lib)
	}
	return byURL, nil
}

func compileEnvelope(engine *Engine, env *types.ResourceEnvelope) (*Library, error) {
	src, url, name, version, err := parseLibraryEnvelope(env)
	if err != nil {
		return nil, err
	}
	return compileLibrarySource(engine, src, url, name, version)
}

func resolveLibraryIndex(byURL map[string][]*Library, canonical string) (*Library, bool, error) {
	url, version := splitCanonical(canonical)
	candidates := byURL[url]
	if len(candidates) == 0 {
		for _, libs := range byURL {
			for _, lib := range libs {
				if matchesCanonical(lib, canonical) {
					candidates = append(candidates, lib)
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}
	if version != "" {
		var matches []*Library
		for _, lib := range candidates {
			if matchesCanonical(lib, canonical) {
				matches = append(matches, lib)
			}
		}
		if len(matches) == 0 {
			return nil, false, nil
		}
		if len(matches) > 1 {
			return nil, false, errf("%w: ambiguous CQL library canonical %s", ErrLibraryNotFound, canonical)
		}
		return matches[0], true, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return versionLess(candidates[j].Version, candidates[i].Version)
	})
	return candidates[0], true, nil
}

func versionLess(a, b string) bool {
	return strings.TrimSpace(a) < strings.TrimSpace(b)
}

// MapLibraryResolver resolves from an in-memory CQL source table.
type MapLibraryResolver map[string]string

func (m MapLibraryResolver) Resolve(_ context.Context, canonical string) (*Library, error) {
	src, ok := m[canonical]
	if !ok {
		url, _ := splitCanonical(canonical)
		src, ok = m[url]
	}
	if !ok || strings.TrimSpace(src) == "" {
		return nil, errf("%w: %s", ErrLibraryNotFound, canonical)
	}
	lib, err := parseLibrary(src)
	if err != nil {
		return nil, err
	}
	lib.URL = canonical
	return lib, nil
}
