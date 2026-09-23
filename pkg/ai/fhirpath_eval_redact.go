package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/types"
)

// redactCompiledFHIRPaths evaluates compiled expressions and redacts matching
// leaf values in the JSON resource map. Simple dot paths are handled via segment
// redaction first; evaluation covers backtick paths and other compile-only forms.
func redactCompiledFHIRPaths(ctx context.Context, engine fhirpath.Engine, resourceType string, root map[string]any, compiled []fhirpath.CompiledExpression, labels []string, placeholder string) ([]string, error) {
	if engine == nil || root == nil || len(compiled) == 0 {
		return nil, nil
	}
	resource, err := jsonResourceForFHIRPath(resourceType, root)
	if err != nil {
		return nil, err
	}
	var redactions []string
	for i, expr := range compiled {
		label := labels[i]
		if label == "" {
			label = expr.Expr()
		}
		if seg := SegmentPathFromFHIRPathExpr(label); seg != "" {
			if n := redactFHIRPathSegments(root, strings.Split(seg, "."), placeholder); n > 0 {
				redactions = append(redactions, fmt.Sprintf("%s.%s", resourceType, seg))
				continue
			}
		}
		items, err := expr.Eval(ctx, resource)
		if err != nil || len(items) == 0 {
			continue
		}
		for _, item := range items {
			if redactMatchingLeaves(root, item, placeholder) {
				redactions = append(redactions, fmt.Sprintf("%s.%s", resourceType, label))
			}
		}
	}
	return uniqueStrings(redactions), nil
}

// redactMatchingLeaves replaces primitive values equal to want with placeholder.
func redactMatchingLeaves(node any, want fhirpath.Value, placeholder string) bool {
	target, ok := primitiveStringFromValue(want)
	if !ok {
		return false
	}
	return redactPrimitiveEqual(node, target, placeholder) > 0
}

func primitiveStringFromValue(v fhirpath.Value) (string, bool) {
	s, err := v.String()
	if err != nil {
		return "", false
	}
	return s, true
}

func redactPrimitiveEqual(node any, target, placeholder string) int {
	switch cur := node.(type) {
	case map[string]any:
		var n int
		for k, v := range cur {
			if prim, ok := v.(string); ok && prim == target {
				cur[k] = placeholder
				n++
				continue
			}
			n += redactPrimitiveEqual(v, target, placeholder)
		}
		return n
	case []any:
		var n int
		for i := range cur {
			n += redactPrimitiveEqual(cur[i], target, placeholder)
		}
		return n
	default:
		return 0
	}
}

func jsonResourceForFHIRPath(resourceType string, root map[string]any) (any, error) {
	data, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	return types.NewJSONCodec().ParseJSON(resourceType, data)
}
