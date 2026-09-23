package ai

import (
	"fmt"
	"sort"
	"strings"
)

// MarkdownContextFormatter renders tool output (FHIR JSON shapes) as Markdown.
type MarkdownContextFormatter struct{}

// NewMarkdownContextFormatter returns a Markdown context formatter.
func NewMarkdownContextFormatter() *MarkdownContextFormatter {
	return &MarkdownContextFormatter{}
}

// Format implements ToolContextFormatter.
func (f *MarkdownContextFormatter) Format(data any) (string, error) {
	if data == nil {
		return "", nil
	}
	var b strings.Builder
	writeMarkdownValue(&b, data, 0)
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", nil
	}
	return out + "\n", nil
}

func writeMarkdownValue(b *strings.Builder, v any, depth int) {
	switch val := v.(type) {
	case map[string]any:
		writeMarkdownMap(b, val, depth)
	case []any:
		writeMarkdownSlice(b, val, depth)
	case string:
		writeMarkdownScalar(b, val)
	case bool:
		writeMarkdownScalar(b, fmt.Sprintf("%t", val))
	case float64:
		if val == float64(int64(val)) {
			writeMarkdownScalar(b, fmt.Sprintf("%d", int64(val)))
		} else {
			writeMarkdownScalar(b, fmt.Sprintf("%g", val))
		}
	case nil:
		writeMarkdownScalar(b, "null")
	default:
		writeMarkdownScalar(b, fmt.Sprintf("%v", val))
	}
}

func writeMarkdownMap(b *strings.Builder, m map[string]any, depth int) {
	if len(m) == 0 {
		writeMarkdownScalar(b, "{}")
		return
	}
	keys := sortedMapKeys(m)
	title := mapMarkdownTitle(m)
	if title != "" && depth == 0 {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteByte('\n')
		b.WriteByte('\n')
	}
	for _, key := range keys {
		child := m[key]
		if isMarkdownScalar(child) {
			b.WriteString("- **")
			b.WriteString(escapeMarkdownInline(key))
			b.WriteString(":** ")
			writeMarkdownInlineValue(b, child)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(markdownHeading(depth+1, key))
		b.WriteByte('\n')
		writeMarkdownValue(b, child, depth+1)
		b.WriteByte('\n')
	}
}

func writeMarkdownSlice(b *strings.Builder, items []any, depth int) {
	if len(items) == 0 {
		writeMarkdownScalar(b, "[]")
		return
	}
	for i, item := range items {
		label := fmt.Sprintf("Item %d", i+1)
		if itemMap, ok := item.(map[string]any); ok {
			if rt, _ := itemMap["resourceType"].(string); rt != "" {
				label = rt
				if id, _ := itemMap["id"].(string); id != "" {
					label = fmt.Sprintf("%s (%s)", rt, id)
				}
			}
		}
		b.WriteString("- **")
		b.WriteString(escapeMarkdownInline(label))
		b.WriteString("**")
		if isMarkdownScalar(item) {
			b.WriteString(": ")
			writeMarkdownInlineValue(b, item)
			b.WriteByte('\n')
			continue
		}
		b.WriteByte('\n')
		writeMarkdownIndentedBlock(b, item, depth+1)
	}
}

func writeMarkdownIndentedBlock(b *strings.Builder, v any, depth int) {
	var inner strings.Builder
	writeMarkdownValue(&inner, v, depth)
	for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
		if line == "" {
			b.WriteByte('\n')
			continue
		}
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func mapMarkdownTitle(m map[string]any) string {
	rt, _ := m["resourceType"].(string)
	if rt == "" {
		if vn, _ := m["viewName"].(string); vn != "" {
			return "View: " + vn
		}
		return ""
	}
	id, _ := m["id"].(string)
	if id != "" {
		return fmt.Sprintf("%s (%s)", rt, id)
	}
	return rt
}

func markdownHeading(depth int, title string) string {
	level := depth + 1
	if level > 6 {
		level = 6
	}
	return strings.Repeat("#", level) + " " + escapeMarkdownInline(title)
}

func isMarkdownScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func writeMarkdownScalar(b *strings.Builder, s string) {
	b.WriteString(escapeMarkdownInline(s))
}

func writeMarkdownInlineValue(b *strings.Builder, v any) {
	switch val := v.(type) {
	case string:
		if strings.ContainsAny(val, "\n\r") {
			b.WriteString("\n\n```\n")
			b.WriteString(val)
			b.WriteString("\n```")
			return
		}
		writeMarkdownScalar(b, val)
	case nil:
		writeMarkdownScalar(b, "null")
	default:
		writeMarkdownScalar(b, fmt.Sprintf("%v", val))
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	priority := map[string]int{
		"resourceType": 0,
		"id":           1,
		"viewName":     0,
		"version":      2,
		"total":        3,
		"count":        4,
		"columns":      5,
		"rows":         6,
		"resources":    7,
		"included":     8,
	}
	sort.SliceStable(keys, func(i, j int) bool {
		pi, pj := priority[keys[i]], priority[keys[j]]
		if pi != pj {
			if pi == 0 && pj != 0 {
				return true
			}
			if pj == 0 && pi != 0 {
				return false
			}
			if pi != 0 && pj != 0 {
				return pi < pj
			}
		}
		return keys[i] < keys[j]
	})
	return keys
}

func escapeMarkdownInline(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
	)
	return replacer.Replace(s)
}
