package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DefinitionExtractContext declares a resource to create during definition-based extraction.
type DefinitionExtractContext struct {
	Definition string
	FullURL    string
}

// DefinitionExtractValue maps a fixed value or expression into an extracted resource element.
type DefinitionExtractValue struct {
	Definition string
	Expression *Expression
	Value      any
	ValueType  string
}

func parseDefinitionExtract(ext Extension) (DefinitionExtractContext, bool) {
	if ext.URL != SDCDefinitionExtractExtension {
		return DefinitionExtractContext{}, false
	}
	ctx := DefinitionExtractContext{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "definition":
			ctx.Definition = extensionScalarString(child)
		case "fullUrl":
			ctx.FullURL = extensionScalarString(child)
		}
	}
	if ctx.Definition == "" {
		if v := extensionScalarString(ext); v != "" {
			ctx.Definition = v
		}
	}
	return ctx, ctx.Definition != ""
}

func parseDefinitionExtractValue(ext Extension) (DefinitionExtractValue, bool) {
	if ext.URL != SDCDefinitionExtractValueExt {
		return DefinitionExtractValue{}, false
	}
	out := DefinitionExtractValue{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "definition":
			out.Definition = extensionScalarString(child)
		case "expression":
			if expression, ok := extensionExpression(child); ok {
				out.Expression = &expression
			}
		default:
			if strings.HasPrefix(childURLSuffix(child.URL), "value") {
				out.Value = child.Value
				out.ValueType = firstNonEmpty(child.ValueType, child.valueType, strings.TrimPrefix(childURLSuffix(child.URL), "value"))
			}
		}
	}
	if out.Definition == "" {
		return DefinitionExtractValue{}, false
	}
	return out, out.Expression != nil || out.Value != nil
}

func definitionExtractExtension(ctx DefinitionExtractContext) Extension {
	ext := Extension{URL: SDCDefinitionExtractExtension, Extension: []Extension{
		{URL: "definition", Value: ctx.Definition, valueType: "Canonical"},
	}}
	if ctx.FullURL != "" {
		ext.Extension = append(ext.Extension, Extension{URL: "fullUrl", Value: ctx.FullURL, valueType: "String"})
	}
	return ext
}

func definitionExtractValueExtension(v DefinitionExtractValue) Extension {
	children := []Extension{{URL: "definition", Value: v.Definition, valueType: "Uri"}}
	if v.Expression != nil {
		children = append(children, Extension{URL: "expression", Value: *v.Expression, valueType: "Expression"})
	}
	if v.Value != nil {
		suffix := firstNonEmpty(v.ValueType, valueSuffix(v.Value))
		children = append(children, Extension{URL: "value" + suffix, Value: v.Value, valueType: suffix})
	}
	return Extension{URL: SDCDefinitionExtractValueExt, Extension: children}
}

func resourceTypeFromCanonical(canonical string) string {
	base := strings.Split(canonical, "|")[0]
	base = strings.TrimSuffix(base, "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		return base[i+1:]
	}
	return base
}

func canonicalBase(canonical string) string {
	return strings.Split(canonical, "|")[0]
}

func definitionExtractMappingsFromQuestionnaire(q Questionnaire, r QuestionnaireResponse) []extractedResource {
	var out []extractedResource
	var walk func([]Item, []ResponseItem)
	walk = func(items []Item, responseItems []ResponseItem) {
		for _, item := range items {
			ri := findResponse(responseItems, item.LinkID)
			if item.DefinitionExtract != nil {
				var responses []ResponseItem
				if ri != nil {
					responses = []ResponseItem{*ri}
				}
				out = append(out, extractedResource{
					LinkID:   item.LinkID,
					Context:  *item.DefinitionExtract,
					Items:    item.Item,
					Response: responses,
				})
			}
			childResponse := []ResponseItem{}
			if ri != nil {
				childResponse = ri.Item
			}
			walk(item.Item, childResponse)
		}
	}
	walk(q.Item, r.Item)
	return out
}

type extractedResource struct {
	LinkID   string
	Context  DefinitionExtractContext
	Items    []Item
	Response []ResponseItem
}

func buildExtractedResource(ctx context.Context, extract extractedResource, r QuestionnaireResponse, provider ExpressionProvider) (map[string]any, error) {
	canonical := canonicalBase(extract.Context.Definition)
	res := map[string]any{"resourceType": resourceTypeFromCanonical(extract.Context.Definition)}
	var walk func([]Item, []ResponseItem)
	walk = func(items []Item, responseItems []ResponseItem) {
		for _, item := range items {
			ri := findResponse(responseItems, item.LinkID)
			if item.Definition != "" && strings.HasPrefix(item.Definition, canonical+"#") {
				if ri != nil {
					for _, answer := range ri.Answer {
						if answerValueAbsent(answer.Value) {
							continue
						}
						_, path, ok := parseCanonicalElementPath(item.Definition)
						if ok {
							setPath(res, path, answer.Value)
						}
					}
				}
			}
			for _, valueDef := range item.DefinitionExtractValues {
				if !strings.HasPrefix(valueDef.Definition, canonical+"#") {
					continue
				}
				_, path, ok := parseCanonicalElementPath(valueDef.Definition)
				if !ok {
					continue
				}
				value := valueDef.Value
				if valueDef.Expression != nil && provider != nil {
					values, err := provider.Evaluate(ctx, *valueDef.Expression, r)
					if err != nil {
						continue
					}
					if len(values) > 0 {
						value = values[0]
					} else {
						continue
					}
				}
				if value != nil {
					setPath(res, path, value)
				}
			}
			childResponse := []ResponseItem{}
			if ri != nil {
				childResponse = ri.Item
			}
			walk(item.Item, childResponse)
		}
	}
	walk(extract.Items, extract.Response)
	return res, nil
}

func definitionExtractBundleEntries(ctx context.Context, q Questionnaire, r QuestionnaireResponse, provider ExpressionProvider) ([]map[string]any, error) {
	extracts := definitionExtractMappingsFromQuestionnaire(q, r)
	if len(extracts) == 0 {
		return nil, nil
	}
	rootScope := allocateExtractIDs(q.Item, r.Item, map[string]any{})
	if provider != nil && len(rootScope) > 0 {
		provider = expressionProviderWithScope(provider, rootScope)
	}
	var entries []map[string]any
	for _, extract := range extracts {
		res, err := buildExtractedResource(ctx, extract, r, provider)
		if err != nil {
			return nil, err
		}
		if len(res) <= 1 {
			continue
		}
		b, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}
		resourceType, _ := res["resourceType"].(string)
		if resourceType == "" {
			return nil, fmt.Errorf("definitionExtract for %q produced a resource without resourceType", extract.LinkID)
		}
		fullURL := "urn:uuid:" + extract.LinkID + "-extract"
		if extract.Context.FullURL != "" && provider != nil {
			values, err := provider.Evaluate(ctx, Expression{Language: "text/fhirpath", Expression: extract.Context.FullURL}, r)
			if err != nil {
				return nil, err
			}
			if len(values) > 0 {
				if s, ok := values[0].(string); ok && s != "" {
					fullURL = s
				}
			}
		}
		entries = append(entries, map[string]any{
			"fullUrl":  fullURL,
			"resource": json.RawMessage(b),
			"request":  map[string]any{"method": "POST", "url": resourceType},
		})
	}
	return entries, nil
}
