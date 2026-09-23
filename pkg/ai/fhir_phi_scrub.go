package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/fhirpath"
)

// scrubResourceMerged redacts PHI in one JSON walk, combining catalog rules,
// indexed FHIRPath segment paths, and precomputed FHIRPath eval targets. Eval
// uses a single marshal/parse per resource before the walk.
func scrubResourceMerged(
	ctx context.Context,
	resourceType string,
	root map[string]any,
	catalog *PHICatalog,
	bundle phiPathBundle,
	engine fhirpath.Engine,
	placeholder string,
) ([]string, error) {
	if root == nil {
		return nil, nil
	}
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	evalTargets, err := buildEvalStringTargets(ctx, engine, resourceType, root, bundle.compiled)
	if err != nil {
		return nil, err
	}
	segmentSet := make(map[string]struct{}, len(bundle.segment))
	for _, seg := range bundle.segment {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			segmentSet[seg] = struct{}{}
		}
	}
	st := &scrubState{
		resourceType: resourceType,
		catalog:      catalog,
		placeholder:  placeholder,
		strict:       catalog.resourceRequiresStrictRedaction(root),
		segmentPaths: segmentSet,
		evalTargets:  evalTargets,
	}
	st.walkMap(root)
	return uniqueStrings(st.redactions), nil
}

type scrubState struct {
	resourceType string
	catalog      *PHICatalog
	placeholder  string
	strict       bool
	path         []string
	segmentPaths map[string]struct{}
	evalTargets  map[string]struct{}
	redactions   []string
}

func (s *scrubState) walkMap(m map[string]any) {
	for key, val := range m {
		if s.redactKey(m, key, val) {
			continue
		}
		s.descend(key, val)
	}
}

func (s *scrubState) redactKey(parent map[string]any, key string, val any) bool {
	fullPath := s.joinPath(append(s.path, key))
	if s.matchesSegmentPath(fullPath) {
		parent[key] = s.placeholder
		s.recordRedaction(fullPath)
		return true
	}
	if s.catalog.pathMatches(s.resourceType, fullPath) {
		parent[key] = s.placeholder
		s.recordRedaction(fullPath)
		return true
	}
	if s.isLeaf(val) {
		if s.catalog.passiveSensitiveKey(key, fullPath) || (s.strict && s.shouldRedactStrictLeaf(key, fullPath)) {
			parent[key] = s.placeholder
			s.recordRedaction(fullPath)
			return true
		}
		if s.matchesEvalTarget(val) {
			parent[key] = s.placeholder
			s.recordRedaction(fullPath)
			return true
		}
	}
	return false
}

func (s *scrubState) matchesSegmentPath(fullPath string) bool {
	if len(s.segmentPaths) == 0 {
		return false
	}
	norm := normalizePathIndexes(fullPath)
	if _, ok := s.segmentPaths[norm]; ok {
		return true
	}
	return false
}

func (s *scrubState) matchesEvalTarget(val any) bool {
	if len(s.evalTargets) == 0 {
		return false
	}
	str, ok := val.(string)
	if !ok || str == "" {
		return false
	}
	_, ok = s.evalTargets[str]
	return ok
}

func (s *scrubState) recordRedaction(fullPath string) {
	s.redactions = append(s.redactions, fmt.Sprintf("%s.%s", s.resourceType, fullPath))
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
			if s.strict && !s.catalog.pathAllowedInStrictMode(fullPath, idx) {
				v[i] = s.placeholder
				s.recordRedaction(fullPath)
				continue
			}
			if s.matchesEvalTarget(item) {
				v[i] = s.placeholder
				s.recordRedaction(fullPath)
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
