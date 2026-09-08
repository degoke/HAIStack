package sdc

import (
	"context"
	"fmt"
	"strings"
)

func validateItemDefinitionConstraintsExtended(item *Item, linkID string, o *Outcome) {
	path := "Questionnaire.item[" + linkID + "]"
	if item.MinValue != nil && !boundValueCompatible(item.Type, *item.MinValue) {
		o.add("error", "type", "minValue is not compatible with item type "+item.Type, path+".extension")
	}
	if item.MaxValue != nil && !boundValueCompatible(item.Type, *item.MaxValue) {
		o.add("error", "type", "maxValue is not compatible with item type "+item.Type, path+".extension")
	}
}

func validateAnswerBounds(o *Outcome, item *Item, answer Answer, path string) {
	if answerValueAbsent(answer.Value) {
		return
	}
	if item.MinValue != nil {
		if less, ok := compareAnswerToBound(answer.Value, item.Type, *item.MinValue); ok && less {
			o.add("error", "min", "answer is less than minValue "+formatBoundValue(*item.MinValue), path)
		}
	}
	if item.MaxValue != nil {
		if exceeds, ok := compareAnswerToBound(item.MaxValue.Value, item.Type, BoundValue{Value: answer.Value, ValueType: valueSuffix(answer.Value)}); ok && exceeds {
			o.add("error", "max", "answer is greater than maxValue "+formatBoundValue(*item.MaxValue), path)
		}
	}
}

func boundValueCompatible(itemType string, bound BoundValue) bool {
	switch itemType {
	case "integer", "decimal":
		_, ok := number(bound.Value)
		return ok
	case "date", "dateTime", "time":
		return formatBoundValue(bound) != ""
	default:
		return false
	}
}

func compareAnswerToBound(answer any, itemType string, bound BoundValue) (bool, bool) {
	switch itemType {
	case "integer", "decimal":
		av, aok := number(answer)
		bv, bok := number(bound.Value)
		if !aok || !bok {
			return false, false
		}
		return av < bv, true
	case "date", "dateTime", "time":
		as, aok := answer.(string)
		bs := formatBoundValue(bound)
		if !aok || bs == "" {
			return false, false
		}
		return as < bs, true
	default:
		return false, false
	}
}

func validateReferenceAnswer(o *Outcome, item *Item, answer Answer, path string) {
	if item.Type != "reference" || answerValueAbsent(answer.Value) {
		return
	}
	ref, ok := referenceFrom(answer.Value)
	if !ok {
		return
	}
	if len(item.ReferenceResources) > 0 {
		rt := ref.Type
		if rt == "" {
			rt = resourceTypeFromReference(ref.Reference)
		}
		allowed := false
		for _, want := range item.ReferenceResources {
			if strings.EqualFold(want, rt) {
				allowed = true
				break
			}
		}
		if !allowed {
			o.add("error", "invalid", "reference type "+rt+" is not permitted", path)
		}
	}
}

func validateQuantityAnswer(o *Outcome, item *Item, answer Answer, opts ValidationOptions, path string) {
	if item.Type != "quantity" || answerValueAbsent(answer.Value) {
		return
	}
	qty, ok := quantityFrom(answer.Value)
	if !ok {
		return
	}
	if item.Unit != nil && item.Unit.Code != "" && qty.Code != "" && qty.Code != item.Unit.Code {
		o.add("error", "invariant", "quantity unit does not match questionnaire-unit", path)
	}
	if len(item.UnitOptions) > 0 && qty.Code != "" {
		matched := false
		for _, option := range item.UnitOptions {
			if option.Code == qty.Code && (option.System == "" || option.System == qty.System) {
				matched = true
				break
			}
		}
		if !matched {
			o.add("error", "code-invalid", "quantity unit is not one of the permitted unit options", path)
		}
	}
	if item.UnitValueSet != "" && opts.Terminology != nil && qty.Code != "" {
		if err := opts.Terminology.ValidateCode(context.Background(), Coding{System: qty.System, Code: qty.Code}, item.UnitValueSet); err != nil {
			o.add("error", "code-invalid", err.Error(), path)
		}
	}
}

func validateChildOccurs(o *Outcome, item Item, responseItem *ResponseItem, path string) {
	if item.Type != "group" && item.Type != "question" {
		return
	}
	counts := map[string]int{}
	for _, child := range responseItem.Item {
		counts[child.LinkID]++
	}
	for _, def := range item.Item {
		count := counts[def.LinkID]
		if def.MinOccurs != nil && count < *def.MinOccurs {
			o.add("error", "min", fmt.Sprintf("item %q requires at least %d occurrences, found %d", def.LinkID, *def.MinOccurs, count), path+".item["+def.LinkID+"]")
		}
		if def.MaxOccurs != nil && count > *def.MaxOccurs {
			o.add("error", "max", fmt.Sprintf("item %q allows at most %d occurrences, found %d", def.LinkID, *def.MaxOccurs, count), path+".item["+def.LinkID+"]")
		}
	}
}

func validateOptionExclusive(o *Outcome, item *Item, responseItem *ResponseItem, path string) {
	if !item.OptionExclusive {
		return
	}
	if countPresentAnswers(responseItem.Answer) > 1 {
		o.add("error", "max", "optionExclusive question allows only one answer", path)
	}
}

func validateRequiredExpression(o *Outcome, item *Item, response QuestionnaireResponse, opts ValidationOptions, path string, hasAnswer bool) {
	if item.RequiredExpression == nil || hasAnswer || opts.AllowIncomplete {
		return
	}
	if opts.Expressions == nil {
		o.add("error", "exception", "required expression provider is unavailable", path)
		return
	}
	values, err := opts.Expressions.Evaluate(context.Background(), *item.RequiredExpression, response)
	if err != nil {
		o.add("error", "exception", err.Error(), path)
		return
	}
	if len(values) > 0 && truthy(values[0]) {
		o.add("error", "required", "required expression evaluated to true but answer is missing", path)
	}
}

func validateAnswerValueSet(o *Outcome, item *Item, answer Answer, opts ValidationOptions, path string) {
	if item.AnswerValueSet == "" || opts.Terminology == nil {
		return
	}
	if item.Type != "choice" && item.Type != "open-choice" {
		return
	}
	if c, ok := codingFrom(answer.Value); ok {
		if err := opts.Terminology.ValidateCode(context.Background(), c, item.AnswerValueSet); err != nil {
			o.add("error", "code-invalid", err.Error(), path)
		}
		return
	}
	if item.Type == "open-choice" {
		if s, ok := answer.Value.(string); ok && s != "" {
			codes, err := opts.Terminology.Expand(context.Background(), item.AnswerValueSet)
			if err != nil {
				o.add("error", "exception", err.Error(), path)
				return
			}
			for _, code := range codes {
				if code.Code == s || code.Display == s {
					return
				}
			}
			o.add("error", "code-invalid", "answer is not in answerValueSet", path)
		}
	}
}

func validateQuestionnaireResponseMetadata(q Questionnaire, r QuestionnaireResponse, o *Outcome) {
	if !q.SignatureRequired {
		return
	}
	if len(r.Signatures) == 0 {
		o.add("error", "required", "questionnaire signature is required", "QuestionnaireResponse.extension")
	}
}

func referenceFrom(v any) (Reference, bool) {
	switch x := v.(type) {
	case Reference:
		return x, x.Reference != ""
	case map[string]any:
		ref := Reference{}
		if s, ok := x["reference"].(string); ok {
			ref.Reference = s
		}
		if s, ok := x["type"].(string); ok {
			ref.Type = s
		}
		if s, ok := x["display"].(string); ok {
			ref.Display = s
		}
		return ref, ref.Reference != ""
	default:
		return Reference{}, false
	}
}

func resourceTypeFromReference(reference string) string {
	if reference == "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(reference, "#"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

type simpleQuantity struct {
	Value  float64
	Code   string
	System string
}

func quantityFrom(v any) (simpleQuantity, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return simpleQuantity{}, false
	}
	q := simpleQuantity{}
	if value, exists := m["value"]; exists {
		n, ok := number(value)
		if !ok {
			return simpleQuantity{}, false
		}
		q.Value = n
	}
	if code, ok := m["code"].(string); ok {
		q.Code = code
	}
	if system, ok := m["system"].(string); ok {
		q.System = system
	}
	return q, true
}
