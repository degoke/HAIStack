package sdc

import (
	"strings"
)

func validatePerformerTypes(q Questionnaire, r QuestionnaireResponse, o *Outcome) {
	if len(q.PerformerTypes) == 0 {
		return
	}
	for _, author := range r.Author {
		rt := author.Type
		if rt == "" {
			rt = resourceTypeFromReference(author.Reference)
		}
		if rt == "" {
			continue
		}
		allowed := false
		for _, want := range q.PerformerTypes {
			if strings.EqualFold(want, rt) {
				allowed = true
				break
			}
		}
		if !allowed {
			o.add("error", "invalid", "response author type "+rt+" is not listed in performerType", "QuestionnaireResponse.extension")
		}
	}
}

func validateItemTargetConstraints(o *Outcome, item *Item, r QuestionnaireResponse, opts ValidationOptions, path string) {
	if item == nil || len(item.TargetConstraints) == 0 {
		return
	}
	if opts.Expressions == nil {
		o.add("error", "exception", "targetConstraint expression provider is unavailable", path)
		return
	}
	for _, constraint := range item.TargetConstraints {
		if constraint.Expression == "" {
			continue
		}
		values, err := opts.Expressions.Evaluate(validationContext(opts), Expression{Language: "text/fhirpath", Expression: constraint.Expression}, r)
		if err != nil {
			o.add("error", "exception", err.Error(), path)
			continue
		}
		if len(values) == 0 || !truthy(values[0]) {
			msg := constraint.Requirements
			if msg == "" {
				msg = "targetConstraint " + constraint.Key + " failed"
			}
			o.add(constraintSeverity(constraint.Severity), "invariant", msg, path)
		}
	}
}
