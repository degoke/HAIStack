package sdc

import (
	"context"
	"fmt"
)

func validateIsSubjectItems(o *Outcome, defs []Item, responses []ResponseItem, path string) {
	for _, def := range defs {
		p := path + "item[" + def.LinkID + "]"
		ri := findResponse(responses, def.LinkID)
		if def.IsSubject {
			if ri == nil {
				o.add("error", "required", "isSubject group requires a response item", p)
			} else if subjectRefFromResponseItem(*ri) == "" {
				o.add("error", "required", "isSubject group requires a subject reference answer", p)
			}
		}
		if ri != nil && ri.IsSubject && subjectRefFromResponseItem(*ri) == "" {
			o.add("error", "required", "isSubject response item requires a subject reference answer", p)
		}
		if len(def.Item) > 0 {
			childResponses := []ResponseItem{}
			if ri != nil {
				childResponses = ri.Item
			}
			validateIsSubjectItems(o, def.Item, childResponses, p+".")
		}
	}
}

func subjectRefFromResponseItem(ri ResponseItem) string {
	if ref, ok := referenceFrom(firstPresentAnswerValue(ri.Answer)); ok {
		return ref.Reference
	}
	for _, child := range ri.Item {
		if ref := subjectRefFromResponseItem(child); ref != "" {
			return ref
		}
	}
	return ""
}

func firstPresentAnswerValue(answers []Answer) any {
	for _, answer := range answers {
		if !answerValueAbsent(answer.Value) {
			return answer.Value
		}
	}
	return nil
}

func validateQuestionnaireTargetConstraints(o *Outcome, q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) {
	for _, constraint := range q.TargetConstraints {
		if constraint.Expression == "" {
			continue
		}
		if opts.Expressions == nil {
			o.add("error", "exception", "targetConstraint expression provider is unavailable", "Questionnaire.extension.targetConstraint")
			continue
		}
		values, err := opts.Expressions.Evaluate(context.Background(), Expression{Language: "text/fhirpath", Expression: constraint.Expression}, r)
		if err != nil {
			o.add("error", "exception", err.Error(), "Questionnaire.extension.targetConstraint")
			continue
		}
		if len(values) == 0 || !truthy(values[0]) {
			msg := constraint.Requirements
			if msg == "" {
				msg = fmt.Sprintf("targetConstraint %s failed", constraint.Key)
			}
			o.add(constraintSeverity(constraint.Severity), "invariant", msg, "Questionnaire.extension.targetConstraint")
		}
	}
}
