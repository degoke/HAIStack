package sdc

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ItemConstraint is a FHIR questionnaire-constraint extension on Questionnaire.item.
type ItemConstraint struct {
	Key          string `json:"key,omitempty"`
	Requirements string `json:"requirements,omitempty"`
	Severity     string `json:"severity,omitempty"`
	Expression   string `json:"expression,omitempty"`
}

func maxLengthPermittedItemType(typ string) bool {
	switch typ {
	case "boolean", "decimal", "integer", "string", "text", "url", "open-choice":
		return true
	default:
		return false
	}
}

func maxLengthEnforcedItemType(typ string) bool {
	switch typ {
	case "string", "text", "url":
		return true
	case "open-choice":
		return true
	default:
		return false
	}
}

func parseItemConstraint(ext Extension) (ItemConstraint, bool) {
	if len(ext.Extension) == 0 {
		return ItemConstraint{}, false
	}
	c := ItemConstraint{}
	for _, child := range ext.Extension {
		switch childURLSuffix(child.URL) {
		case "key":
			c.Key = extensionScalarString(child)
		case "requirements":
			c.Requirements = extensionScalarString(child)
		case "severity":
			c.Severity = extensionScalarString(child)
		case "expression":
			c.Expression = extensionScalarString(child)
		}
	}
	if c.Key == "" && c.Expression == "" {
		return ItemConstraint{}, false
	}
	return c, true
}

func itemConstraintExtension(c ItemConstraint) Extension {
	children := make([]Extension, 0, 4)
	if c.Key != "" {
		children = append(children, Extension{URL: "key", Value: c.Key, valueType: "Id"})
	}
	if c.Requirements != "" {
		children = append(children, Extension{URL: "requirements", Value: c.Requirements, valueType: "String"})
	}
	if c.Severity != "" {
		children = append(children, Extension{URL: "severity", Value: c.Severity, valueType: "Code"})
	}
	if c.Expression != "" {
		children = append(children, Extension{URL: "expression", Value: c.Expression, valueType: "String"})
	}
	return Extension{URL: QuestionnaireConstraintExtension, Extension: children}
}

func validateItemDefinitionConstraints(item *Item, linkID string, o *Outcome) {
	path := "Questionnaire.item[" + linkID + "]"
	if item.MaxLength != nil {
		if !maxLengthPermittedItemType(item.Type) {
			o.add("error", "invariant", "maxLength is not permitted for item type "+item.Type, path+".maxLength")
		} else if *item.MaxLength <= 0 {
			o.add("error", "value", "maxLength must be positive", path+".maxLength")
		}
	}
	if item.Regex != "" {
		if _, err := regexp.Compile(item.Regex); err != nil {
			o.add("error", "value", "regex extension is not a valid regular expression: "+err.Error(), path+".extension")
		}
	}
	if item.MinLength != nil && *item.MinLength <= 0 {
		o.add("error", "value", "minLength must be positive", path+".extension")
	}
	if item.MinOccurs != nil && *item.MinOccurs < 0 {
		o.add("error", "value", "minOccurs must be non-negative", path+".extension")
	}
	if item.MaxOccurs != nil && *item.MaxOccurs < 0 {
		o.add("error", "value", "maxOccurs must be non-negative", path+".extension")
	}
	if item.MinOccurs != nil && item.MaxOccurs != nil && *item.MinOccurs > *item.MaxOccurs {
		o.add("error", "invariant", "minOccurs must be less than or equal to maxOccurs", path+".extension")
	}
	validateItemDefinitionConstraintsExtended(item, linkID, o)
	for _, constraint := range item.Constraints {
		constraintPath := path + ".extension.questionnaire-constraint[" + constraint.Key + "]"
		if constraint.Key == "" {
			o.add("error", "required", "questionnaire-constraint key is required", constraintPath)
		}
		if constraint.Expression == "" {
			o.add("error", "required", "questionnaire-constraint expression is required", constraintPath)
		}
		if constraint.Severity != "" && constraint.Severity != "error" && constraint.Severity != "warning" {
			o.add("error", "value", "questionnaire-constraint severity must be error or warning", constraintPath)
		}
	}
}

func validateAnswerValueConstraints(o *Outcome, item *Item, answer Answer, path string) {
	if answerValueAbsent(answer.Value) {
		return
	}
	text, ok := answerStringForConstraints(item.Type, answer.Value)
	if !ok {
		return
	}
	if item.MaxLength != nil && maxLengthEnforcedItemType(item.Type) && len(text) > *item.MaxLength {
		o.add("error", "max", fmt.Sprintf("answer exceeds maxLength %d", *item.MaxLength), path)
	}
	if item.MinLength != nil && maxLengthEnforcedItemType(item.Type) && len(text) < *item.MinLength {
		o.add("error", "min", fmt.Sprintf("answer is shorter than minLength %d", *item.MinLength), path)
	}
	if item.Regex != "" {
		pattern, err := regexp.Compile(item.Regex)
		if err != nil {
			o.add("error", "exception", "regex extension is not a valid regular expression: "+err.Error(), path)
			return
		}
		if !pattern.MatchString(text) {
			o.add("error", "invariant", "answer does not match the required pattern", path)
		}
	}
}

func validateItemInvariantConstraints(o *Outcome, item *Item, q Questionnaire, response QuestionnaireResponse, opts ValidationOptions, path string) {
	if len(item.Constraints) == 0 {
		return
	}
	ancestors := questionnaireAncestors(q, item.LinkID)
	provider := expressionProviderWithAncestors(context.Background(), opts.Expressions, ancestors, response)
	if provider == nil {
		for _, constraint := range item.Constraints {
			if constraint.Expression != "" {
				o.add("error", "exception", "questionnaire-constraint expression provider is unavailable", path)
				return
			}
		}
		return
	}
	for _, constraint := range item.Constraints {
		if constraint.Expression == "" {
			continue
		}
		values, err := provider.Evaluate(context.Background(), Expression{
			Language:   "text/fhirpath",
			Expression: constraint.Expression,
		}, response)
		if err != nil {
			o.add("error", "exception", err.Error(), path)
			continue
		}
		if len(values) == 0 || !truthy(values[0]) {
			msg := strings.TrimSpace(constraint.Requirements)
			if msg == "" {
				msg = "questionnaire constraint failed"
				if constraint.Key != "" {
					msg = "questionnaire constraint " + constraint.Key + " failed"
				}
			}
			o.add(constraintSeverity(constraint.Severity), "invariant", msg, path)
		}
	}
}

func constraintSeverity(severity string) string {
	if strings.EqualFold(severity, "warning") {
		return "warning"
	}
	return "error"
}

func answerStringForConstraints(itemType string, value any) (string, bool) {
	switch itemType {
	case "string", "text", "url":
		s, ok := value.(string)
		return s, ok
	case "open-choice":
		if s, ok := value.(string); ok {
			return s, ok
		}
		return "", false
	default:
		return "", false
	}
}
