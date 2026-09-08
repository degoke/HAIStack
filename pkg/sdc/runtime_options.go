package sdc

import (
	"context"
	"fmt"
	"strings"
)

func effectiveAnswerOptions(ctx context.Context, q Questionnaire, item Item, r QuestionnaireResponse, opts ValidationOptions) []AnswerOption {
	options := append([]AnswerOption(nil), item.AnswerOption...)
	if len(options) == 0 {
		return options
	}
	ancestors := questionnaireAncestors(q, item.LinkID)
	provider := expressionProviderWithAncestors(ctx, opts.Expressions, ancestors, r)
	if provider == nil {
		return options
	}
	for i := range options {
		if options[i].ToggleExpression == nil {
			continue
		}
		values, err := provider.Evaluate(ctx, *options[i].ToggleExpression, r)
		if err != nil || len(values) == 0 || !truthy(values[0]) {
			options[i].Disabled = true
		}
	}
	return options
}

func evaluateCandidates(ctx context.Context, q Questionnaire, item Item, r QuestionnaireResponse, opts ValidationOptions) []any {
	if item.CandidateExpression == nil || opts.Expressions == nil {
		return nil
	}
	ancestors := questionnaireAncestors(q, item.LinkID)
	provider := expressionProviderWithAncestors(ctx, opts.Expressions, ancestors, r)
	if provider == nil {
		return nil
	}
	values, err := provider.Evaluate(ctx, *item.CandidateExpression, r)
	if err != nil {
		return nil
	}
	return values
}

func evaluateContextExpressions(ctx context.Context, q Questionnaire, item Item, r QuestionnaireResponse, opts ValidationOptions) ([]ContextResourceResult, []Issue) {
	if len(item.ContextExpressions) == 0 {
		return nil, nil
	}
	if opts.Expressions == nil {
		return nil, contextExpressionIssues(item, "context expression evaluation is unavailable")
	}
	ancestors := questionnaireAncestors(q, item.LinkID)
	provider := expressionProviderWithAncestors(ctx, opts.Expressions, ancestors, r)
	provider = expressionProviderWithScope(provider, map[string]any{"qitem": item})
	if provider == nil {
		return nil, contextExpressionIssues(item, "context expression evaluation is unavailable")
	}
	var results []ContextResourceResult
	var issues []Issue
	for _, expr := range item.ContextExpressions {
		if !strings.EqualFold(expr.Expression.Language, FHIRQueryLanguage) {
			issues = append(issues, contextExpressionIssue(item, fmt.Sprintf("unsupported context expression language %q", expr.Expression.Language)))
			continue
		}
		values, err := provider.Evaluate(ctx, expr.Expression, r)
		if err != nil {
			issues = append(issues, contextExpressionIssue(item, err.Error()))
			continue
		}
		if len(values) == 0 {
			results = append(results, ContextResourceResult{Label: expr.Label})
			continue
		}
		results = append(results, ContextResourceResult{Label: expr.Label, Resources: values})
	}
	return results, issues
}

func contextExpressionIssues(item Item, diagnostics string) []Issue {
	return []Issue{contextExpressionIssue(item, diagnostics)}
}

func contextExpressionIssue(item Item, diagnostics string) Issue {
	return Issue{
		Severity:    "warning",
		Code:        "exception",
		Diagnostics: diagnostics,
		FieldPath:   "item[" + item.LinkID + "]",
	}
}

func validateAnswerOptionsEnabled(o *Outcome, item *Item, q Questionnaire, r QuestionnaireResponse, opts ValidationOptions, path string) {
	if len(item.AnswerOption) == 0 || opts.Expressions == nil {
		return
	}
	options := effectiveAnswerOptions(context.Background(), q, *item, r, opts)
	disabled := map[int]bool{}
	for i, option := range options {
		if option.Disabled {
			disabled[i] = true
		}
	}
	if len(disabled) == 0 {
		return
	}
	ri := findResponseDeep(r.Item, item.LinkID)
	if ri == nil {
		return
	}
	for _, answer := range ri.Answer {
		if answerValueAbsent(answer.Value) {
			continue
		}
		for i, option := range item.AnswerOption {
			if !disabled[i] {
				continue
			}
			if Equal(option.Value, answer.Value) {
				o.add("error", "code-invalid", "answer selects a disabled answer option", path)
				return
			}
		}
	}
}
