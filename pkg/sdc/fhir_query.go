package sdc

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const FHIRQueryLanguage = "application/x-fhir-query"

// FHIRQuerySearch executes FHIR search for the SDC FHIR Query adapter.
type FHIRQuerySearch interface {
	Search(ctx context.Context, resourceType string, params url.Values) ([]*types.ResourceEnvelope, error)
}

// SearchFHIRQueryProvider executes FHIR Query expressions against a local search service.
type SearchFHIRQueryProvider struct {
	Search FHIRQuerySearch
}

// NewSearchFHIRQueryProvider constructs a provider backed by pkg/search.Service.
func NewSearchFHIRQueryProvider(svc *search.Service) SearchFHIRQueryProvider {
	return SearchFHIRQueryProvider{Search: searchServiceFHIRQuery{svc: svc}}
}

type searchServiceFHIRQuery struct {
	svc *search.Service
}

func (a searchServiceFHIRQuery) Search(ctx context.Context, resourceType string, params url.Values) ([]*types.ResourceEnvelope, error) {
	if a.svc == nil {
		return nil, fmt.Errorf("FHIR Query search service is unavailable")
	}
	result, err := a.svc.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// ExecuteFHIRQuery parses a FHIR REST query and returns matching resources.
// Callers that need SDC %variable substitution should use executeFHIRQueryWithConstants
// or evaluate through a contextual expression provider.
func (p SearchFHIRQueryProvider) ExecuteFHIRQuery(ctx context.Context, query string, input any) ([]any, error) {
	if p.Search == nil {
		return nil, fmt.Errorf("FHIR Query search service is unavailable")
	}
	if constants := fhirQueryConstantsFromInput(input); len(constants) > 0 {
		return executeFHIRQueryWithConstants(ctx, p, query, constants, input)
	}
	resourceType, params, err := parseFHIRQuery(query)
	if err != nil {
		return nil, err
	}
	result, err := p.Search.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(result))
	for i, res := range result {
		out[i] = res
	}
	return out, nil
}

// ExecuteFHIRQueryWithConstants substitutes %variables from constants before executing the query.
func (p SearchFHIRQueryProvider) ExecuteFHIRQueryWithConstants(ctx context.Context, query string, constants map[string]any) ([]any, error) {
	return executeFHIRQueryWithConstants(ctx, p, query, constants, nil)
}

func executeFHIRQueryWithConstants(ctx context.Context, provider FHIRQueryProvider, query string, constants map[string]any, input any) ([]any, error) {
	substituted, err := substituteFHIRQueryConstants(query, constants)
	if err != nil {
		return nil, err
	}
	if searchable, ok := provider.(SearchFHIRQueryProvider); ok {
		return searchable.executePreparedQuery(ctx, substituted)
	}
	return provider.ExecuteFHIRQuery(ctx, substituted, input)
}

func (p SearchFHIRQueryProvider) executePreparedQuery(ctx context.Context, query string) ([]any, error) {
	if p.Search == nil {
		return nil, fmt.Errorf("FHIR Query search service is unavailable")
	}
	resourceType, params, err := parseFHIRQuery(query)
	if err != nil {
		return nil, err
	}
	result, err := p.Search.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(result))
	for i, res := range result {
		out[i] = res
	}
	return out, nil
}

func isFHIRQueryExpression(e Expression) bool {
	return strings.EqualFold(e.Language, FHIRQueryLanguage)
}

func fhirQueryConstantsFromInput(input any) map[string]any {
	switch x := input.(type) {
	case ExpressionEnvironment:
		return x.Constants
	case map[string]any:
		if constants, ok := x["constants"].(map[string]any); ok {
			return constants
		}
	}
	return nil
}

// MultiExpressionProvider routes expressions to language-specific providers.
type MultiExpressionProvider struct {
	FHIRPath  ExpressionProvider
	FHIRQuery ExpressionProvider
	CQL       ExpressionProvider
}

// ComposeExpressions builds a multi-language expression provider.
func ComposeExpressions(fhirpath ExpressionProvider, fhirQuery FHIRQueryProvider, cql CQLProvider) ExpressionProvider {
	var queryExpr ExpressionProvider
	if fhirQuery != nil {
		queryExpr = FHIRQueryExpressions{Provider: fhirQuery}
	}
	var cqlExpr ExpressionProvider
	if cql != nil {
		cqlExpr = CQLExpressions{Provider: cql}
	}
	if fhirpath == nil && queryExpr == nil && cqlExpr == nil {
		return nil
	}
	return MultiExpressionProvider{
		FHIRPath:  fhirpath,
		FHIRQuery: queryExpr,
		CQL:       cqlExpr,
	}
}

func (p MultiExpressionProvider) Evaluate(ctx context.Context, e Expression, input any) ([]any, error) {
	switch strings.ToLower(e.Language) {
	case "text/fhirpath":
		if p.FHIRPath != nil {
			return p.FHIRPath.Evaluate(ctx, e, input)
		}
	case FHIRQueryLanguage:
		if p.FHIRQuery != nil {
			return p.FHIRQuery.Evaluate(ctx, e, input)
		}
	case "text/cql":
		if p.CQL != nil {
			return p.CQL.Evaluate(ctx, e, input)
		}
	}
	return UnsupportedProvider{e.Language}.Evaluate(ctx, e, input)
}

func parseFHIRQuery(query string) (string, url.Values, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("FHIR Query expression is empty")
	}
	if idx := strings.Index(query, "://"); idx >= 0 {
		if slash := strings.Index(query[idx+3:], "/"); slash >= 0 {
			query = query[idx+3+slash:]
		}
	}
	query = strings.TrimPrefix(query, "/")
	parts := strings.SplitN(query, "?", 2)
	resourceType := strings.Trim(parts[0], "/")
	if strings.Contains(resourceType, "/") {
		segments := strings.Split(resourceType, "/")
		resourceType = segments[len(segments)-1]
	}
	if resourceType == "" {
		return "", nil, fmt.Errorf("FHIR Query is missing a resource type")
	}
	params := url.Values{}
	if len(parts) == 2 && parts[1] != "" {
		parsed, err := url.ParseQuery(parts[1])
		if err != nil {
			return "", nil, fmt.Errorf("FHIR Query parameters: %w", err)
		}
		params = parsed
	}
	return resourceType, params, nil
}

func substituteFHIRQueryConstants(query string, constants map[string]any) (string, error) {
	var b strings.Builder
	for i := 0; i < len(query); i++ {
		if query[i] != '%' {
			b.WriteByte(query[i])
			continue
		}
		j := i + 1
		for j < len(query) {
			c := query[j]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
				j++
				continue
			}
			break
		}
		if j == i+1 {
			b.WriteByte('%')
			continue
		}
		name := query[i+1 : j]
		value, ok := lookupFHIRQueryConstant(name, constants)
		if !ok {
			return "", fmt.Errorf("FHIR Query references unknown context variable %%%s", name)
		}
		b.WriteString(url.QueryEscape(fhirQuerySubstituteValue(value)))
		i = j - 1
	}
	return b.String(), nil
}

func fhirQuerySubstituteValue(value any) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		return x
	case Reference:
		if x.Reference != "" {
			return x.Reference
		}
		if x.Type != "" {
			return x.Type
		}
	case map[string]any:
		if ref, ok := x["reference"].(string); ok && ref != "" {
			return ref
		}
		rt, _ := x["resourceType"].(string)
		id, _ := x["id"].(string)
		if rt != "" && id != "" {
			return rt + "/" + id
		}
	case *types.ResourceEnvelope:
		if x != nil && x.ResourceType != "" && x.ID != "" {
			return x.ResourceType + "/" + x.ID
		}
	case Item:
		if x.LinkID != "" {
			return x.LinkID
		}
	}
	return fmt.Sprint(value)
}

func lookupFHIRQueryConstant(name string, constants map[string]any) (any, bool) {
	if value, ok := constants[name]; ok {
		return value, true
	}
	if name == "patient" {
		if value, ok := constants["subject"]; ok {
			return value, true
		}
	}
	return nil, false
}

func extractFHIRQueryProvider(provider ExpressionProvider) FHIRQueryProvider {
	switch p := provider.(type) {
	case FHIRQueryExpressions:
		return p.Provider
	case MultiExpressionProvider:
		if p.FHIRQuery != nil {
			return extractFHIRQueryProvider(p.FHIRQuery)
		}
		return nil
	case contextualExpressionProvider:
		return extractFHIRQueryProvider(p.base)
	case scopedExpressionProvider:
		return extractFHIRQueryProvider(p.inner)
	default:
		return nil
	}
}
