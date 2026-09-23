package ai

import (
	"context"
	"fmt"
	"strings"
)

type scrubOptions struct {
	toolName string
	evalMode EvalMode
}

func effectiveEvalMode(mode EvalMode, toolName string) EvalMode {
	if mode == "" {
		mode = EvalModeKeywordsOnly
	}
	if toolName == ToolSearchFhirResources && mode != EvalModeAlways {
		return EvalModeNever
	}
	return mode
}

// scrubResourceMerged redacts PHI in one JSON walk, combining indexed FHIRPath
// segment paths, catalog rules, and optional FHIRPath eval targets.
func scrubResourceMerged(
	ctx context.Context,
	resourceType string,
	root map[string]any,
	catalog *PHICatalog,
	bundle phiPathBundle,
	placeholder string,
	opts scrubOptions,
) ([]string, error) {
	if root == nil {
		return nil, nil
	}
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	_ = ctx
	evalMode := effectiveEvalMode(opts.evalMode, opts.toolName)
	segmentIdx := bundle.segmentIdx
	if evalMode != EvalModeNever && len(bundle.labels) > 0 {
		segmentIdx = mergePathIndices(segmentIdx, buildEvalPathIndex(bundle.labels))
	}
	st := &scrubState{
		resourceType: resourceType,
		catalog:      catalog,
		placeholder:  placeholder,
		strict:       catalog.resourceRequiresStrictRedaction(root),
		segmentIdx:   segmentIdx,
		catalogIdx:   bundle.catalogIdx,
		redactionSet: make(map[string]struct{}),
	}
	st.walkMap(root)
	return st.redactionList(), nil
}

type scrubState struct {
	resourceType string
	catalog      *PHICatalog
	placeholder  string
	strict       bool
	path         []string
	segmentIdx   *pathIndex
	catalogIdx   *pathIndex
	redactionSet map[string]struct{}
}

func (s *scrubState) redactionList() []string {
	if len(s.redactionSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.redactionSet))
	for k := range s.redactionSet {
		out = append(out, k)
	}
	return out
}

func (s *scrubState) recordRedaction(fullPath string) {
	s.redactionSet[fmt.Sprintf("%s.%s", s.resourceType, fullPath)] = struct{}{}
}

func (s *scrubState) walkMap(m map[string]any) {
	for key, val := range m {
		if s.redactKey(m, key, val) {
			continue
		}
		s.descend(key, val)
	}
}

func (s *scrubState) pathIndicesMatch(norm string) bool {
	if s.segmentIdx != nil && s.segmentIdx.Match(norm) {
		return true
	}
	return s.catalogIdx != nil && s.catalogIdx.Match(norm)
}

func (s *scrubState) redactKey(parent map[string]any, key string, val any) bool {
	fullPath := s.joinPath(append(s.path, key))
	norm := normalizePathIndexes(fullPath)
	if s.pathIndicesMatch(norm) {
		parent[key] = s.placeholder
		s.recordRedaction(fullPath)
		return true
	}
	if s.isLeaf(val) {
		if s.catalog.passiveSensitiveKey(key, norm) || (s.strict && s.shouldRedactStrictLeaf(key, norm)) {
			parent[key] = s.placeholder
			s.recordRedaction(fullPath)
			return true
		}
	}
	return false
}

func (s *scrubState) descend(key string, val any) {
	s.path = append(s.path, key)
	defer func() { s.path = s.path[:len(s.path)-1] }()

	switch v := val.(type) {
	case map[string]any:
		s.walkMap(v)
	case []any:
		for i, item := range v {
			idx := fmt.Sprintf("%d", i)
			if itemMap, ok := item.(map[string]any); ok {
				s.path = append(s.path, idx)
				s.walkMap(itemMap)
				s.path = s.path[:len(s.path)-1]
				continue
			}
			fullPath := s.joinPath(append(s.path, idx))
			norm := normalizePathIndexes(fullPath)
			if s.strict && !s.catalog.pathAllowedInStrictMode(norm, idx) {
				v[i] = s.placeholder
				s.recordRedaction(fullPath)
				continue
			}
		}
	}
}

func (s *scrubState) isLeaf(val any) bool {
	switch val.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func (s *scrubState) shouldRedactStrictLeaf(key, fullPath string) bool {
	if s.catalog.pathAllowedInStrictMode(fullPath, key) {
		return false
	}
	if s.catalog.alwaysAllowedKey(key) {
		return false
	}
	return true
}

func (s *scrubState) joinPath(parts []string) string {
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

func normalizePathIndexes(path string) string {
	if path == "" {
		return ""
	}
	var out []string
	for _, p := range strings.Split(path, ".") {
		if p == "" {
			continue
		}
		if isPathIndexSegment(p) {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

func isPathIndexSegment(part string) bool {
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return false
		}
	}
	return len(part) > 0
}
