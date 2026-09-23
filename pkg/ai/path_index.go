package ai

import (
	"sort"
	"strings"
)

// pathIndex supports fast exact and prefix path matching for scrubbing.
type pathIndex struct {
	exact    map[string]struct{}
	prefixes []string // longest first
}

func newPathIndex(paths []string) *pathIndex {
	if len(paths) == 0 {
		return nil
	}
	exact := make(map[string]struct{})
	var prefixes []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		exact[p] = struct{}{}
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		return len(prefixes[i]) > len(prefixes[j])
	})
	return &pathIndex{exact: exact, prefixes: prefixes}
}

func mergePathIndices(parts ...*pathIndex) *pathIndex {
	var all []string
	for _, p := range parts {
		if p == nil {
			continue
		}
		for k := range p.exact {
			all = append(all, k)
		}
	}
	return newPathIndex(all)
}

func (idx *pathIndex) Match(normPath string) bool {
	if idx == nil || normPath == "" {
		return false
	}
	if _, ok := idx.exact[normPath]; ok {
		return true
	}
	for _, prefix := range idx.prefixes {
		if normPath == prefix || strings.HasPrefix(normPath, prefix+".") {
			return true
		}
	}
	return false
}

func catalogPathIndex(catalog *PHICatalog, resourceType string) *pathIndex {
	if catalog == nil {
		return nil
	}
	var paths []string
	for _, suffix := range catalog.globalPathSuffixes() {
		paths = append(paths, suffix)
	}
	for _, suffix := range catalog.ResourcePathSuffixes[resourceType] {
		paths = append(paths, suffix)
	}
	for _, el := range catalog.ElementsForResource(resourceType) {
		paths = append(paths, el)
	}
	return newPathIndex(paths)
}
