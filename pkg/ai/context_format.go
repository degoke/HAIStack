package ai

// ToolContextFormatter converts structured tool output into model-facing text.
type ToolContextFormatter interface {
	Format(data any) (string, error)
}

// ContextFormat selects a built-in ToolContextFormatter.
type ContextFormat string

const (
	// ContextFormatJSON uses indented JSON (default).
	ContextFormatJSON ContextFormat = "json"
	// ContextFormatMarkdown renders structured FHIR/tool data as Markdown.
	ContextFormatMarkdown ContextFormat = "markdown"
)

// NewToolContextFormatter returns a formatter for the given kind. Unknown values
// default to JSON.
func NewToolContextFormatter(kind ContextFormat) ToolContextFormatter {
	switch kind {
	case ContextFormatMarkdown:
		return NewMarkdownContextFormatter()
	default:
		return NewJSONContextFormatter()
	}
}
