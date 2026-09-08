package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	SDCExtractAllocateIDExt      = SDCBaseURL + "sdc-questionnaire-extractAllocateId"
	SDCTemplateExtractExt        = SDCBaseURL + "sdc-questionnaire-templateExtract"
	SDCTemplateExtractValueExt   = SDCBaseURL + "sdc-questionnaire-templateExtractValue"
	SDCTemplateExtractContextExt = SDCBaseURL + "sdc-questionnaire-templateExtractContext"
	SDCTemplateExtractBundleExt  = SDCBaseURL + "sdc-questionnaire-templateExtractBundle"
	SDCAssembleContextExt        = SDCBaseURL + "sdc-questionnaire-assembleContext"
	SDCAssembleExpectationExt    = SDCBaseURL + "sdc-questionnaire-assemble-expectation"
	SDCContextExpressionExt      = SDCBaseURL + "sdc-questionnaire-contextExpression"
	SDCPerformerTypeExt          = SDCBaseURL + "sdc-questionnaire-performerType"
	SDCChoiceColumnExt           = SDCBaseURL + "sdc-questionnaire-choiceColumn"
	SDCItemOptionalDisplayExt    = SDCBaseURL + "sdc-questionnaire-itemOptionalDisplay"
	SDCCQFLibraryExt             = SDCBaseURL + "sdc-questionnaire-cqf-library"
)

func parseTemplateExtract(ext Extension) (TemplateExtractContext, bool) {
	if ext.URL != SDCTemplateExtractExt {
		return TemplateExtractContext{}, false
	}
	ctx := TemplateExtractContext{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "template":
			if ref, ok := extensionReference(child); ok {
				ctx.TemplateReference = ref.Reference
			} else {
				ctx.TemplateReference = extensionScalarString(child)
			}
		case "fullUrl":
			ctx.FullURL = extensionScalarString(child)
		case "resourceId":
			ctx.ResourceID = extensionScalarString(child)
		}
	}
	return ctx, ctx.TemplateReference != ""
}

func templateExtractBundleExtension(ctx TemplateExtractContext) Extension {
	ext := templateExtractExtension(ctx)
	ext.URL = SDCTemplateExtractBundleExt
	return ext
}

func templateExtractExtension(ctx TemplateExtractContext) Extension {
	children := []Extension{{URL: "template", Value: Reference{Reference: ctx.TemplateReference}, valueType: "Reference"}}
	if ctx.FullURL != "" {
		children = append(children, Extension{URL: "fullUrl", Value: ctx.FullURL, valueType: "String"})
	}
	if ctx.ResourceID != "" {
		children = append(children, Extension{URL: "resourceId", Value: ctx.ResourceID, valueType: "String"})
	}
	return Extension{URL: SDCTemplateExtractExt, Extension: children}
}

func parseTemplateExtractValue(ext Extension) (TemplateExtractValue, bool) {
	if ext.URL != SDCTemplateExtractValueExt {
		return TemplateExtractValue{}, false
	}
	out := TemplateExtractValue{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "path":
			out.Path = extensionScalarString(child)
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
	return out, out.Path != "" && (out.Expression != nil || out.Value != nil)
}

func templateExtractValueExtension(v TemplateExtractValue) Extension {
	children := []Extension{{URL: "path", Value: v.Path, valueType: "String"}}
	if v.Expression != nil {
		children = append(children, Extension{URL: "expression", Value: *v.Expression, valueType: "Expression"})
	}
	if v.Value != nil {
		suffix := firstNonEmpty(v.ValueType, valueSuffix(v.Value))
		children = append(children, Extension{URL: "value" + suffix, Value: v.Value, valueType: suffix})
	}
	return Extension{URL: SDCTemplateExtractValueExt, Extension: children}
}

func parseContextExpression(ext Extension) (ContextExpression, bool) {
	if ext.URL != SDCContextExpressionExt {
		return ContextExpression{}, false
	}
	out := ContextExpression{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "expression":
			if expression, ok := extensionExpression(child); ok {
				out.Expression = expression
			}
		case "label":
			out.Label = extensionScalarString(child)
		}
	}
	return out, out.Expression.Expression != ""
}

func contextExpressionExtension(v ContextExpression) Extension {
	return Extension{URL: SDCContextExpressionExt, Extension: []Extension{
		{URL: "expression", Value: v.Expression, valueType: "Expression"},
		{URL: "label", Value: v.Label, valueType: "String"},
	}}
}

func parseChoiceColumn(ext Extension) (ChoiceColumn, bool) {
	if ext.URL != SDCChoiceColumnExt {
		return ChoiceColumn{}, false
	}
	out := ChoiceColumn{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "path":
			out.Path = extensionScalarString(child)
		case "label":
			out.Label = extensionScalarString(child)
		case "forDisplay":
			out.ForDisplay = extensionBoolValue(child)
		}
	}
	return out, out.Path != ""
}

func choiceColumnExtension(v ChoiceColumn) Extension {
	return Extension{URL: SDCChoiceColumnExt, Extension: []Extension{
		{URL: "path", Value: v.Path, valueType: "String"},
		{URL: "label", Value: v.Label, valueType: "String"},
		{URL: "forDisplay", Value: v.ForDisplay, valueType: "Boolean"},
	}}
}

func parseCQFLibrary(ext Extension) (CQFLibraryRef, bool) {
	if ext.URL != SDCCQFLibraryExt {
		return CQFLibraryRef{}, false
	}
	out := CQFLibraryRef{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "library":
			out.LibraryCanonical = extensionScalarString(child)
		case "name":
			out.Name = extensionScalarString(child)
		}
	}
	return out, out.LibraryCanonical != ""
}

func cqfLibraryExtension(v CQFLibraryRef) Extension {
	return Extension{URL: SDCCQFLibraryExt, Extension: []Extension{
		{URL: "library", Value: v.LibraryCanonical, valueType: "Canonical"},
		{URL: "name", Value: v.Name, valueType: "Code"},
	}}
}

func containedResource(q Questionnaire, ref string) (map[string]any, bool) {
	ref = strings.TrimPrefix(ref, "#")
	for _, resource := range q.Contained {
		if id, ok := resource["id"].(string); ok && id == ref {
			return resource, true
		}
	}
	return nil, false
}

func cloneMap(value map[string]any) map[string]any {
	b, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func templateExtractBundleEntries(ctx context.Context, q Questionnaire, r QuestionnaireResponse, provider ExpressionProvider) ([]map[string]any, error) {
	var entries []map[string]any
	rootScope := allocateExtractIDs(q.Item, r.Item, map[string]any{})
	if q.TemplateExtractBundle != nil {
		bundleEntries, err := extractContainedBundle(ctx, q, r, *q.TemplateExtractBundle, provider, rootScope)
		if err != nil {
			return nil, err
		}
		entries = append(entries, bundleEntries...)
	}
	var walk func([]Item, []ResponseItem, map[string]any, any)
	walk = func(items []Item, responseItems []ResponseItem, allocScope map[string]any, evalRoot any) {
		for _, item := range items {
			ri := findResponse(responseItems, item.LinkID)
			scope := extractScopeForItem(q, item, responseItems, allocScope)
			root := evalRoot
			if ri != nil {
				root = *ri
			}
			providerForItem := provider
			if provider != nil && len(scope) > 0 {
				providerForItem = expressionProviderWithScope(provider, scope)
			}
			if item.TemplateExtract != nil {
				template, ok := containedResource(q, item.TemplateExtract.TemplateReference)
				if !ok {
					continue
				}
				cloned := cloneMap(template)
				if cloned == nil {
					continue
				}
				delete(cloned, "id")
				applyTemplateExtractValues(ctx, cloned, item.TemplateExtractValues, providerForItem, root)
				applyEmbeddedTemplateExtensions(ctx, cloned, providerForItem, root)
				for _, child := range item.Item {
					if child.Definition == "" {
						continue
					}
					childRI := findResponse(responseItems, child.LinkID)
					if childRI == nil || len(childRI.Answer) == 0 || answerValueAbsent(childRI.Answer[0].Value) {
						continue
					}
					_, path, ok := parseCanonicalElementPath(child.Definition)
					if ok {
						setPath(cloned, path, childRI.Answer[0].Value)
					}
				}
				b, err := json.Marshal(cloned)
				if err != nil {
					continue
				}
				resourceType, _ := cloned["resourceType"].(string)
				if resourceType == "" {
					continue
				}
				fullURL := "urn:uuid:" + item.LinkID + "-template"
				if item.TemplateExtract.FullURL != "" && providerForItem != nil {
					values, err := providerForItem.Evaluate(ctx, Expression{Language: "text/fhirpath", Expression: item.TemplateExtract.FullURL}, root)
					if err == nil && len(values) > 0 {
						if s, ok := values[0].(string); ok && s != "" {
							fullURL = s
						}
					}
				}
				method := "POST"
				url := resourceType
				if item.TemplateExtract.ResourceID != "" && providerForItem != nil {
					values, err := providerForItem.Evaluate(ctx, Expression{Language: "text/fhirpath", Expression: item.TemplateExtract.ResourceID}, root)
					if err == nil && len(values) > 0 {
						if id, ok := values[0].(string); ok && id != "" {
							cloned["id"] = id
							method = "PUT"
							url = resourceType + "/" + id
							b, _ = json.Marshal(cloned)
						}
					}
				}
				entries = append(entries, map[string]any{
					"fullUrl":  fullURL,
					"resource": json.RawMessage(b),
					"request":  map[string]any{"method": method, "url": url},
				})
			}
			childResponse := []ResponseItem{}
			if ri != nil {
				childResponse = ri.Item
			}
			walk(item.Item, childResponse, scope, root)
		}
	}
	walk(q.Item, r.Item, map[string]any{}, r)
	return entries, nil
}

func applyTemplateExtractValues(ctx context.Context, resource map[string]any, values []TemplateExtractValue, provider ExpressionProvider, root any) {
	for _, valueDef := range values {
		v := valueDef.Value
		if valueDef.Expression != nil && provider != nil {
			results, err := provider.Evaluate(ctx, *valueDef.Expression, root)
			if err != nil || len(results) == 0 {
				continue
			}
			v = results[0]
		}
		if v != nil && valueDef.Path != "" {
			setPath(resource, valueDef.Path, v)
		}
	}
}

func applyEmbeddedTemplateExtensions(ctx context.Context, resource map[string]any, provider ExpressionProvider, root any) {
	applyTemplateExtensionsOnNode(ctx, resource, resource, provider, root)
}

func applyTemplateExtensionsOnNode(ctx context.Context, resource map[string]any, node any, provider ExpressionProvider, root any) {
	switch value := node.(type) {
	case map[string]any:
		if extensions, ok := value["extension"].([]any); ok {
			evalRoot := root
			for _, raw := range extensions {
				extMap, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				url, _ := extMap["url"].(string)
				if url != SDCTemplateExtractContextExt {
					continue
				}
				if contextExpr, ok := embeddedTemplateExtractContext(extMap); ok && provider != nil {
					if results, err := provider.Evaluate(ctx, contextExpr, root); err == nil && len(results) > 0 {
						evalRoot = results[0]
					}
				}
			}
			for _, raw := range extensions {
				extMap, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				url, _ := extMap["url"].(string)
				if url != SDCTemplateExtractValueExt {
					continue
				}
				if path, expression, fixed, ok := embeddedTemplateExtractValue(extMap); ok {
					v := fixed
					if expression != nil && provider != nil {
						results, err := provider.Evaluate(ctx, *expression, evalRoot)
						if err != nil || len(results) == 0 {
							continue
						}
						v = results[0]
					}
					if v != nil {
						setPath(resource, path, v)
					}
				}
			}
			filtered := make([]any, 0, len(extensions))
			for _, raw := range extensions {
				extMap, ok := raw.(map[string]any)
				if !ok {
					filtered = append(filtered, raw)
					continue
				}
				url, _ := extMap["url"].(string)
				if url == SDCTemplateExtractValueExt || url == SDCTemplateExtractContextExt {
					continue
				}
				filtered = append(filtered, raw)
			}
			if len(filtered) == 0 {
				delete(value, "extension")
			} else {
				value["extension"] = filtered
			}
		}
		for _, child := range value {
			applyTemplateExtensionsOnNode(ctx, resource, child, provider, root)
		}
	case []any:
		for _, child := range value {
			applyTemplateExtensionsOnNode(ctx, resource, child, provider, root)
		}
	}
}

func embeddedTemplateExtractValue(ext map[string]any) (path string, expression *Expression, fixed any, ok bool) {
	children, _ := ext["extension"].([]any)
	for _, raw := range children {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		url, _ := child["url"].(string)
		switch childURLSuffix(url) {
		case "path":
			path, _ = child["valueString"].(string)
		case "expression":
			if exprMap, ok := child["valueExpression"].(map[string]any); ok {
				expression = &Expression{
					Language:   fmt.Sprint(exprMap["language"]),
					Expression: fmt.Sprint(exprMap["expression"]),
				}
			}
		default:
			for key, value := range child {
				if strings.HasPrefix(key, "value") && key != "valueExpression" {
					fixed = value
				}
			}
		}
	}
	return path, expression, fixed, path != ""
}

func embeddedTemplateExtractContext(ext map[string]any) (Expression, bool) {
	children, _ := ext["extension"].([]any)
	for _, raw := range children {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if childURLSuffix(child["url"].(string)) == "expression" {
			if exprMap, ok := child["valueExpression"].(map[string]any); ok {
				return Expression{
					Language:   fmt.Sprint(exprMap["language"]),
					Expression: fmt.Sprint(exprMap["expression"]),
				}, true
			}
		}
	}
	if exprMap, ok := ext["valueExpression"].(map[string]any); ok {
		return Expression{
			Language:   fmt.Sprint(exprMap["language"]),
			Expression: fmt.Sprint(exprMap["expression"]),
		}, true
	}
	return Expression{}, false
}

func extractContainedBundle(ctx context.Context, q Questionnaire, r QuestionnaireResponse, spec TemplateExtractContext, provider ExpressionProvider, scope map[string]any) ([]map[string]any, error) {
	template, ok := containedResource(q, spec.TemplateReference)
	if !ok {
		return nil, nil
	}
	if rt, _ := template["resourceType"].(string); rt != "Bundle" {
		return nil, fmt.Errorf("templateExtractBundle must reference a contained Bundle")
	}
	cloned := cloneMap(template)
	if cloned == nil {
		return nil, nil
	}
	providerForBundle := provider
	if provider != nil && len(scope) > 0 {
		providerForBundle = expressionProviderWithScope(provider, scope)
	}
	applyEmbeddedTemplateExtensions(ctx, cloned, providerForBundle, r)
	rawEntries, _ := cloned["entry"].([]any)
	var entries []map[string]any
	for _, raw := range rawEntries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resource, _ := entry["resource"].(map[string]any)
		if resource == nil {
			continue
		}
		delete(resource, "id")
		b, err := json.Marshal(resource)
		if err != nil {
			continue
		}
		resourceType, _ := resource["resourceType"].(string)
		if resourceType == "" {
			continue
		}
		fullURL := ""
		if s, ok := entry["fullUrl"].(string); ok {
			fullURL = s
		}
		if fullURL == "" {
			fullURL = "urn:uuid:template-bundle-entry"
		}
		method := "POST"
		url := resourceType
		entries = append(entries, map[string]any{
			"fullUrl":  fullURL,
			"resource": json.RawMessage(b),
			"request":  map[string]any{"method": method, "url": url},
		})
	}
	return entries, nil
}
