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

// scrubResourceMerged redacts PHI by marshaling the resource once, walking JSON
// with jsonparser, applying path-index and catalog rules, then merging back into
// the input map.
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
	strict := catalog.resourceRequiresStrictRedaction(root)
	scrubbed, redactions, err := scrubJSONResource(resourceType, data, catalog, segmentIdx, bundle.catalogIdx, placeholder, strict)
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
