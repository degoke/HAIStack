package ai

import (
	"strings"
)

func effectiveEvalMode(mode EvalMode, toolName string) EvalMode {
	if mode == "" {
		mode = EvalModeKeywordsOnly
	}
	if toolName == ToolSearchFhirResources && mode != EvalModeAlways {
		return EvalModeNever
	}
	return mode
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
