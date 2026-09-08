package sdc

import "context"

func questionnaireAncestors(q Questionnaire, linkID string) []Item {
	var ancestors []Item
	var walk func([]Item, []Item) bool
	walk = func(items []Item, path []Item) bool {
		for _, item := range items {
			current := append(path, item)
			if item.LinkID == linkID {
				if len(current) > 1 {
					ancestors = append([]Item(nil), current[:len(current)-1]...)
				}
				return true
			}
			if walk(item.Item, current) {
				return true
			}
		}
		return false
	}
	walk(q.Item, nil)
	return ancestors
}

func evaluateItemExpressionScope(ctx context.Context, ancestors []Item, provider ExpressionProvider, input any) map[string]any {
	scope := map[string]any{}
	if provider == nil || len(ancestors) == 0 {
		return scope
	}
	for _, item := range ancestors {
		for _, variable := range item.Variables {
			if variable.Name == "" || variable.Expression.Expression == "" {
				continue
			}
			values, err := provider.Evaluate(ctx, variable.Expression, input)
			if err == nil && len(values) > 0 {
				scope[variable.Name] = values[0]
			}
		}
		if item.ItemPopulationContext != nil && item.ItemPopulationContext.Name != "" {
			values, err := provider.Evaluate(ctx, *item.ItemPopulationContext, input)
			if err == nil && len(values) > 0 {
				scope[item.ItemPopulationContext.Name] = values[0]
			}
		}
	}
	return scope
}

func expressionProviderWithAncestors(ctx context.Context, provider ExpressionProvider, ancestors []Item, input any) ExpressionProvider {
	if provider == nil || len(ancestors) == 0 {
		return provider
	}
	scope := evaluateItemExpressionScope(ctx, ancestors, provider, input)
	return expressionProviderWithScope(provider, scope)
}

func expressionProviderWithScope(provider ExpressionProvider, scope map[string]any) ExpressionProvider {
	if provider == nil || len(scope) == 0 {
		return provider
	}
	if cp, ok := provider.(contextualExpressionProvider); ok {
		return cp.withScope(scope)
	}
	return scopedExpressionProvider{inner: provider, scope: scope}
}

func (e ExpressionEnvironment) withScope(scope map[string]any) ExpressionEnvironment {
	out := e
	out.Constants = map[string]any{}
	for name, value := range e.Constants {
		out.Constants[name] = value
	}
	for name, value := range scope {
		if name != "" {
			out.Constants[name] = value
		}
	}
	return out
}

func (p contextualExpressionProvider) withScope(scope map[string]any) ExpressionProvider {
	if len(scope) == 0 {
		return p
	}
	cp := p
	cp.env = p.env.withScope(scope)
	return cp
}

type scopedExpressionProvider struct {
	inner ExpressionProvider
	scope map[string]any
}

func (p scopedExpressionProvider) Evaluate(ctx context.Context, e Expression, input any) ([]any, error) {
	return expressionProviderWithScope(p.inner, p.scope).Evaluate(ctx, e, input)
}
