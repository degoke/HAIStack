package ai

import (
	"context"
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

// scrubResourceMerged marshals root once, scrubs JSON in one walk (including
// nested contained / Bundle entries), then merges bytes back into root.
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
	data, err := marshalJSONPooled(root)
	if err != nil {
		return nil, err
	}
	resolve := func(rt string, resourceJSON []byte) (*pathIndex, *pathIndex, bool, error) {
		if rt == "" {
			rt = resourceTypeFromJSON(resourceJSON, resourceType)
		}
		strict := strictRedactionFromJSON(resourceJSON, catalog)
		if resourceTypeFromJSON(resourceJSON, resourceType) == resourceType && rt == resourceType {
			seg := segmentIdx
			if seg == nil {
				seg = catalogPathIndex(catalog, rt)
			}
			cat := bundle.catalogIdx
			if cat == nil {
				cat = catalogPathIndex(catalog, rt)
			}
			return seg, cat, strict, nil
		}
		idx := catalogPathIndex(catalog, rt)
		return idx, idx, strict, nil
	}
	scrubbed, redactions, err := scrubJSONDocument(data, catalog, placeholder, resourceType, resolve)
	if err != nil {
		return nil, err
	}
	if err := mergeJSONIntoMap(root, scrubbed); err != nil {
		return nil, err
	}
	return redactions, nil
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
