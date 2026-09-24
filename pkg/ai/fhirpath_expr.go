package ai

import "strings"

// fhirPathKeywordElements are JSON property names that are reserved in FHIRPath
// and must be escaped with backticks when compiling dot-path expressions.
var fhirPathKeywordElements = map[string]bool{
	"div": true, "class": true, "in": true, "as": true, "is": true, "contains": true,
}

// NarrativeDivFHIRPath is the compiled FHIRPath expression for Narrative.div.
const NarrativeDivFHIRPath = "text.`div`"

// NarrativeDivSegmentPath is the JSON segment path for Narrative.div (segment redaction).
const NarrativeDivSegmentPath = "text.div"

// NormalizeFHIRPathForCompile converts a dot-separated element path (for example
// text.div) into a FHIRPath source string (for example text.`div`).
func NormalizeFHIRPathForCompile(dotPath string) string {
	dotPath = strings.TrimSpace(dotPath)
	if dotPath == "" {
		return ""
	}
	if strings.Contains(dotPath, "`") || strings.Contains(dotPath, "(") {
		return dotPath
	}
	parts := strings.Split(dotPath, ".")
	for i, part := range parts {
		if fhirPathKeywordElements[strings.ToLower(part)] {
			parts[i] = "`" + part + "`"
		}
	}
	return strings.Join(parts, ".")
}

// SegmentPathFromFHIRPathExpr returns a dot path for JSON segment redaction when
// the expression is a simple property path (possibly with backticks).
func SegmentPathFromFHIRPathExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" || strings.Contains(expr, "(") {
		return ""
	}
	parts := strings.Split(expr, ".")
	var out []string
	for _, part := range parts {
		part = strings.Trim(part, "`")
		if part == "" {
			return ""
		}
		out = append(out, part)
	}
	return strings.Join(out, ".")
}

// ExpandFHIRPathExpressions adds compile-friendly variants for keyword segments.
func ExpandFHIRPathExpressions(paths []string) (segment []string, compile []string) {
	seenSeg := make(map[string]struct{})
	seenCmp := make(map[string]struct{})
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if seg := SegmentPathFromFHIRPathExpr(p); seg != "" {
			if _, ok := seenSeg[seg]; !ok {
				seenSeg[seg] = struct{}{}
				segment = append(segment, seg)
			}
		}
		if norm := NormalizeFHIRPathForCompile(p); norm != "" {
			if _, ok := seenCmp[norm]; !ok {
				seenCmp[norm] = struct{}{}
				compile = append(compile, norm)
			}
		}
	}
	hasText := false
	for s := range seenSeg {
		if s == "text" || strings.HasPrefix(s, "text.") {
			hasText = true
			break
		}
	}
	if hasText {
		if _, ok := seenSeg[NarrativeDivSegmentPath]; !ok {
			segment = append(segment, NarrativeDivSegmentPath)
		}
		if _, ok := seenCmp[NarrativeDivFHIRPath]; !ok {
			compile = append(compile, NarrativeDivFHIRPath)
		}
	}
	return segment, compile
}
