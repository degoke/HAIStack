package ai

import (
	"fmt"
	"sort"
	"strings"
)

// redactFHIRPaths applies FHIRPath expressions to a JSON resource map by
// redacting every matching location. Expressions must be relative to the resource
// root (for example "name.family", "telecom.value"). Paths are compiled when the
// PHI index is built; segment redaction is applied here without mutating unrelated
// nodes.
func redactFHIRPaths(resourceType string, root map[string]any, expressions []string, placeholder string) []string {
	if root == nil || len(expressions) == 0 {
		return nil
	}
	sorted := append([]string(nil), expressions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.Count(sorted[i], ".") > strings.Count(sorted[j], ".")
	})
	var redactions []string
	for _, expr := range sorted {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			continue
		}
		n := redactFHIRPathSegments(root, strings.Split(expr, "."), placeholder)
		if n > 0 {
			redactions = append(redactions, fmt.Sprintf("%s.%s", resourceType, expr))
		}
	}
	return uniqueStrings(redactions)
}

func redactFHIRPathSegments(node any, parts []string, placeholder string) int {
	if node == nil || len(parts) == 0 {
		return 0
	}
	key := parts[0]
	rest := parts[1:]

	switch cur := node.(type) {
	case map[string]any:
		if len(rest) == 0 {
			if _, ok := cur[key]; ok {
				cur[key] = placeholder
				return 1
			}
			return 0
		}
		child, ok := cur[key]
		if !ok {
			return 0
		}
		return redactFHIRPathSegments(child, rest, placeholder)
	case []any:
		var count int
		for i := range cur {
			count += redactFHIRPathSegments(cur[i], parts, placeholder)
		}
		return count
	default:
		return 0
	}
}
