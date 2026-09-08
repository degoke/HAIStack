package sdc

import "context"

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
