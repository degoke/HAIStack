package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolContextFormat selects how tool results are returned to the model in Harness.Chat.
type ToolContextFormat string

const (
	// ToolContextJSON uses executor ContextFormatter output (indented JSON).
	ToolContextJSON ToolContextFormat = "json"
	// ToolContextMarkdown renders read/search/view rows as markdown for models.
	ToolContextMarkdown ToolContextFormat = "markdown"
)

// MarkdownContextBuilder formats executor tool data as markdown summaries.
type MarkdownContextBuilder struct{}

// NewMarkdownContextBuilder returns a markdown context builder.
func NewMarkdownContextBuilder() *MarkdownContextBuilder {
	return &MarkdownContextBuilder{}
}

// FormatToolResult renders tool output for model consumption.
func (b *MarkdownContextBuilder) FormatToolResult(toolName string, data any, citations []Citation) (string, error) {
	if b == nil {
		b = NewMarkdownContextBuilder()
	}
	switch toolName {
	case ToolRunView, ToolGetPatientSummary, ToolGetUpcomingAppointments:
		return formatViewMarkdown(data, citations)
	case ToolSearchFhirResources, ToolSearchPatientByPhone:
		return formatSearchMarkdown(data, citations)
	case ToolReadFhirResource:
		return formatReadMarkdown(data, citations)
	case ToolCreateFhirResource, ToolUpdateFhirResource:
		return formatWriteMarkdown(data, citations)
	default:
		if data == nil {
			return "", nil
		}
		out, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

func formatViewMarkdown(data any, citations []Citation) (string, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return fallbackJSON(data)
	}
	viewName := stringField(m, "viewName")
	version := stringField(m, "version")
	var b strings.Builder
	if viewName != "" {
		b.WriteString("### View: ")
		b.WriteString(viewName)
		if version != "" {
			b.WriteString(" (")
			b.WriteString(version)
			b.WriteByte(')')
		}
		b.WriteByte('\n')
	}
	columns := columnNamesFromData(m["columns"])
	rows, _ := m["rows"].([]map[string]any)
	if rows == nil {
		if raw, ok := m["rows"].([]any); ok {
			rows = make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				if row, ok := item.(map[string]any); ok {
					rows = append(rows, row)
				}
			}
		}
	}
	if len(columns) == 0 && len(rows) > 0 {
		for k := range rows[0] {
			columns = append(columns, k)
		}
	}
	if len(rows) == 0 {
		b.WriteString("_No rows._\n")
	} else {
		writeMarkdownTable(&b, columns, rows)
	}
	if total, ok := intFromAny(m["total"]); ok {
		b.WriteString(fmt.Sprintf("\n_Total rows: %d_\n", total))
	}
	appendCitationLines(&b, citations)
	return b.String(), nil
}

func formatSearchMarkdown(data any, citations []Citation) (string, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return fallbackJSON(data)
	}
	rt := stringField(m, "resourceType")
	var b strings.Builder
	b.WriteString("### Search: ")
	b.WriteString(rt)
	b.WriteByte('\n')
	if total, ok := intFromAny(m["total"]); ok {
		b.WriteString(fmt.Sprintf("Matches: %d\n\n", total))
	}
	resources := sliceOfMaps(m["resources"])
	if len(resources) == 0 {
		b.WriteString("_No resources._\n")
	} else {
		for i, res := range resources {
			id := stringField(res, "id")
			b.WriteString(fmt.Sprintf("%d. **%s/%s**", i+1, rt, id))
			if name := humanNameLine(res["name"]); name != "" {
				b.WriteString(" — ")
				b.WriteString(name)
			}
			b.WriteByte('\n')
		}
	}
	appendCitationLines(&b, citations)
	return b.String(), nil
}

func formatReadMarkdown(data any, citations []Citation) (string, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return fallbackJSON(data)
	}
	rt := stringField(m, "resourceType")
	id := stringField(m, "id")
	var b strings.Builder
	b.WriteString("### ")
	b.WriteString(rt)
	if id != "" {
		b.WriteString("/")
		b.WriteString(id)
	}
	b.WriteString("\n\n")
	if name := humanNameLine(m["name"]); name != "" {
		b.WriteString("- **Name:** ")
		b.WriteString(name)
		b.WriteByte('\n')
	}
	if gender := stringField(m, "gender"); gender != "" {
		b.WriteString("- **Gender:** ")
		b.WriteString(gender)
		b.WriteByte('\n')
	}
	if birth := stringField(m, "birthDate"); birth != "" {
		b.WriteString("- **Birth date:** ")
		b.WriteString(birth)
		b.WriteByte('\n')
	}
	appendCitationLines(&b, citations)
	return b.String(), nil
}

func formatWriteMarkdown(data any, citations []Citation) (string, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return fallbackJSON(data)
	}
	var b strings.Builder
	b.WriteString("### Write committed\n")
	b.WriteString(fmt.Sprintf("- **Operation:** %s\n", stringField(m, "operation")))
	b.WriteString(fmt.Sprintf("- **Resource:** %s/%s\n", stringField(m, "resourceType"), stringField(m, "id")))
	appendCitationLines(&b, citations)
	return b.String(), nil
}

func writeMarkdownTable(b *strings.Builder, columns []string, rows []map[string]any) {
	if len(columns) == 0 {
		return
	}
	b.WriteByte('|')
	for _, col := range columns {
		b.WriteString(" ")
		b.WriteString(col)
		b.WriteString(" |")
	}
	b.WriteByte('\n')
	b.WriteByte('|')
	for range columns {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')
	for _, row := range rows {
		b.WriteByte('|')
		for _, col := range columns {
			b.WriteString(" ")
			b.WriteString(cellString(row[col]))
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}
}

func columnNamesFromData(raw any) []string {
	switch cols := raw.(type) {
	case []string:
		return cols
	case []any:
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			switch col := c.(type) {
			case string:
				names = append(names, col)
			case map[string]any:
				if n := stringField(col, "name"); n != "" {
					names = append(names, n)
				} else if n := stringField(col, "Name"); n != "" {
					names = append(names, n)
				}
			}
		}
		return names
	default:
		return nil
	}
}

func sliceOfMaps(raw any) []map[string]any {
	switch items := raw.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func humanNameLine(raw any) string {
	names := sliceOfMapsFromAny(raw)
	if len(names) == 0 {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					names = append(names, m)
				}
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	n := names[0]
	family := stringField(n, "family")
	given := stringSliceField(n, "given")
	text := strings.Join(given, " ")
	if family != "" {
		if text != "" {
			text += " "
		}
		text += family
	}
	return text
}

func sliceOfMapsFromAny(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return []map[string]any{m}
	}
	return sliceOfMaps(raw)
}

func stringSliceField(m map[string]any, key string) []string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	switch vals := raw.(type) {
	case []string:
		return vals
	case []any:
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(v)
	}
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func cellString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.ReplaceAll(s, "|", "\\|")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return strings.ReplaceAll(string(b), "|", "\\|")
	}
}

func appendCitationLines(b *strings.Builder, citations []Citation) {
	if len(citations) == 0 {
		return
	}
	b.WriteString("\n**Sources:**\n")
	for _, c := range citations {
		if c.Ref != "" {
			b.WriteString("- ")
			b.WriteString(c.Ref)
			b.WriteByte('\n')
			continue
		}
		if c.Kind != "" {
			b.WriteString("- ")
			b.WriteString(c.Kind)
			b.WriteByte('\n')
		}
	}
}

func fallbackJSON(data any) (string, error) {
	if data == nil {
		return "", nil
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
