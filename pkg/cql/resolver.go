package cql

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// StoreLibraryResolver loads FHIR Library resources from a ResourceStore and
// optionally a DefinitionStore, then compiles their CQL content.
//
// Resolve peeks url/name/version on listed Library resources so in-place URL
// retargets are visible, compiles only matching candidates, and caches compiled
// libraries by resource ID and envelope Hash.
type StoreLibraryResolver struct {
	Resources store.ResourceStore
	Registry  store.DefinitionStore
	Engine    *Engine
	mu        sync.Mutex
	compiled  map[string]cachedLibrary
	meta      map[string]libraryMeta
	// compileCount is the number of CQL compiles performed (tests).
	compileCount int
}

type cachedLibrary struct {
	hash string
	lib  *Library
}

type libraryMeta struct {
	hash, url, name, version string
}

func (r *StoreLibraryResolver) Resolve(ctx context.Context, canonical string) (*Library, error) {
	if canonical == "" {
		return nil, errf("%w: library canonical URL is required", ErrLibraryNotFound)
	}
	if r.Engine == nil {
		return nil, ErrEngineUnavailable
	}
	if r.Resources != nil {
		libs, err := r.resolveFromStore(ctx, canonical)
		if err != nil {
			return nil, err
		}
		if lib, ok, resolveErr := resolveCompiledLibraries(libs, canonical); resolveErr != nil {
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

func (r *StoreLibraryResolver) resolveFromStore(ctx context.Context, canonical string) ([]*Library, error) {
	ids, err := r.Resources.ListIDs(ctx, "Library", 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("list Library resources: %w", err)
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	r.mu.Lock()
	for id := range r.meta {
		if !idSet[id] {
			delete(r.meta, id)
			delete(r.compiled, id)
		}
	}
	r.mu.Unlock()

	var out []*Library
	var lastErr error
	for _, id := range ids {
		env, meta, err := r.readMeta(ctx, id)
		if err != nil || env == nil {
			continue
		}
		if !matchesLibraryMeta(meta.url, meta.name, meta.version, canonical) {
			continue
		}
		lib, err := r.compileCached(env)
		if err != nil {
			lastErr = err
			continue
		}
		if lib != nil {
			out = append(out, lib)
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

func (r *StoreLibraryResolver) readMeta(ctx context.Context, id string) (*types.ResourceEnvelope, libraryMeta, error) {
	env, err := r.Resources.Read(ctx, "Library", id)
	if err != nil || env == nil {
		return env, libraryMeta{}, err
	}
	url, name, version := peekLibraryMeta(env)
	meta := libraryMeta{hash: env.Hash, url: url, name: name, version: version}
	r.mu.Lock()
	if r.meta == nil {
		r.meta = map[string]libraryMeta{}
	}
	r.meta[id] = meta
	r.mu.Unlock()
	return env, meta, nil
}

func (r *StoreLibraryResolver) compileCached(env *types.ResourceEnvelope) (*Library, error) {
	if env == nil {
		return nil, nil
	}
	key := env.ID
	if key == "" {
		key = env.Hash
	}
	r.mu.Lock()
	if r.compiled == nil {
		r.compiled = map[string]cachedLibrary{}
	}
	if key != "" {
		if cached, ok := r.compiled[key]; ok && cached.hash == env.Hash && cached.lib != nil {
			lib := cached.lib
			r.mu.Unlock()
			return lib, nil
		}
	}
	r.mu.Unlock()

	lib, err := compileEnvelope(r.Engine, env)
	if err != nil {
		return nil, err
	}
	if key == "" || lib == nil {
		return lib, nil
	}
	r.mu.Lock()
	r.compiled[key] = cachedLibrary{hash: env.Hash, lib: lib}
	r.compileCount++
	r.mu.Unlock()
	return lib, nil
}

func compileEnvelope(engine *Engine, env *types.ResourceEnvelope) (*Library, error) {
	src, url, name, version, err := parseLibraryEnvelope(env)
	if err != nil {
		return nil, err
	}
	return compileLibrarySource(engine, src, url, name, version)
}

func resolveCompiledLibraries(libs []*Library, canonical string) (*Library, bool, error) {
	byURL := map[string][]*Library{}
	for _, lib := range libs {
		if lib == nil {
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
	return resolveLibraryIndex(byURL, canonical)
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
		return compareVersion(candidates[i].Version, candidates[j].Version) > 0
	})
	return candidates[0], true, nil
}

func matchesLibraryMeta(url, name, version, canonical string) bool {
	return matchesCanonical(&Library{URL: url, Name: name, Version: version}, canonical)
}

// compareVersion returns -1, 0, or 1 using numeric dotted-version order
// (so 1.10.0 > 1.9.0). Prerelease tags sort before the matching release.
// Non-numeric leftovers fall back to lexical compare.
func compareVersion(a, b string) int {
	an, ap := versionParts(a)
	bn, bp := versionParts(b)
	n := len(an)
	if len(bn) > n {
		n = len(bn)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	if ap == "" && bp != "" {
		return 1
	}
	if ap != "" && bp == "" {
		return -1
	}
	return strings.Compare(ap, bp)
}

func versionParts(v string) (nums []int, pre string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, ""
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			pre = part + pre
			nums = append(nums, 0)
			continue
		}
		nums = append(nums, n)
	}
	return nums, pre
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
