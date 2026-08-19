package ai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// CitationBuilder constructs provenance citations from tool outputs.
type CitationBuilder struct{}

// NewCitationBuilder returns a citation builder.
func NewCitationBuilder() *CitationBuilder {
	return &CitationBuilder{}
}

// ResourceRef builds a resource citation.
func (b *CitationBuilder) ResourceRef(resourceType, id string) Citation {
	return Citation{
		Kind: "resource",
		Ref:  fmt.Sprintf("%s/%s", resourceType, id),
	}
}

// SearchCitations builds citations for search results and allowed parameters.
func (b *CitationBuilder) SearchCitations(resourceType string, params url.Values, resources []*types.ResourceEnvelope) []Citation {
	return b.SearchCitationsWithIncludes(resourceType, params, resources, nil)
}

// SearchCitationsWithIncludes builds citations for primary and authorized
// included resources. Parameter names are sorted for deterministic output and
// values are intentionally excluded.
func (b *CitationBuilder) SearchCitationsWithIncludes(resourceType string, params url.Values, resources, included []*types.ResourceEnvelope) []Citation {
	citations := make([]Citation, 0, len(resources)+len(included)+1)
	paramNames := make([]string, 0, len(params))
	for key := range params {
		paramNames = append(paramNames, paramBaseName(key))
	}
	slices.Sort(paramNames)
	if len(paramNames) > 0 {
		detail := map[string]string{"resourceType": resourceType}
		for i, name := range paramNames {
			detail[fmt.Sprintf("param%d", i+1)] = name
		}
		citations = append(citations, Citation{
			Kind:   "search",
			Detail: detail,
		})
	}
	for _, res := range resources {
		citations = append(citations, b.ResourceRef(res.ResourceType, res.ID))
	}
	for _, res := range included {
		citations = append(citations, b.ResourceRef(res.ResourceType, res.ID))
	}
	return citations
}

// ViewCitations builds citations for a view execution result.
func (b *CitationBuilder) ViewCitations(viewName, version, resourceType string, columns []string, rows []map[string]any) []Citation {
	detail := map[string]string{
		"viewName":     viewName,
		"viewVersion":  version,
		"resourceType": resourceType,
	}
	for i, col := range columns {
		detail[fmt.Sprintf("column%d", i+1)] = col
	}
	citations := []Citation{{
		Kind:   "view",
		Detail: detail,
	}}
	for _, row := range rows {
		if id, ok := row["id"].(string); ok && id != "" {
			citations = append(citations, b.ResourceRef(resourceType, id))
		}
	}
	return citations
}

// WriteCitation builds a citation for a committed write.
func (b *CitationBuilder) WriteCitation(operation, resourceType, id string) Citation {
	return Citation{
		Kind: "write",
		Ref:  fmt.Sprintf("%s/%s", resourceType, id),
		Detail: map[string]string{
			"operation":    operation,
			"resourceType": resourceType,
		},
	}
}

// ContextFormatter converts tool output into model-facing context text.
type ContextFormatter struct{}

// NewContextFormatter returns a context formatter.
func NewContextFormatter() *ContextFormatter {
	return &ContextFormatter{}
}

// Format serializes data as indented JSON for model consumption.
func (f *ContextFormatter) Format(data any) (string, error) {
	if data == nil {
		return "", nil
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
