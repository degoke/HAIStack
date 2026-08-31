package sdc

import "fmt"

// AnswerOptionValueError is returned when an answer option's Go value does not
// match the FHIR value[x] type expected for its parent question item.
type AnswerOptionValueError struct {
	ItemType  string
	ValueType string
	Reason    string
}

func (e AnswerOptionValueError) Error() string {
	if e.ItemType != "" {
		if e.ValueType != "" {
			return fmt.Sprintf("answer option value for %q item (value%s): %s", e.ItemType, e.ValueType, e.Reason)
		}
		return fmt.Sprintf("answer option value for %q item: %s", e.ItemType, e.Reason)
	}
	if e.ValueType != "" {
		return fmt.Sprintf("answer option value (value%s): %s", e.ValueType, e.Reason)
	}
	return fmt.Sprintf("answer option value: %s", e.Reason)
}

// CodingOption constructs an answer option with a FHIR Coding value.
func CodingOption(code, display string, system ...string) AnswerOption {
	c := Coding{Code: code, Display: display}
	if len(system) > 0 {
		c.System = system[0]
	}
	return AnswerOption{Value: c, ValueType: "Coding"}
}

// StringOption constructs an answer option with a FHIR string value.
func StringOption(s string) AnswerOption {
	return AnswerOption{Value: s, ValueType: "String"}
}

func validateAnswerOptionValue(itemType string, opt AnswerOption) error {
	if opt.Value == nil {
		return AnswerOptionValueError{ItemType: itemType, Reason: "value is required"}
	}
	valueType := firstNonEmpty(opt.ValueType, opt.valueType)
	if valueType == "" {
		valueType = itemValueType(itemType, opt.Value)
	}
	switch itemType {
	case "choice":
		if valueType != "Coding" {
			return AnswerOptionValueError{
				ItemType:  itemType,
				ValueType: valueType,
				Reason:    "expected valueCoding",
			}
		}
		if _, ok := codingFrom(opt.Value); !ok {
			return AnswerOptionValueError{
				ItemType:  itemType,
				ValueType: valueType,
				Reason:    "valueCoding must be a Coding with a non-empty code",
			}
		}
	case "open-choice":
		switch valueType {
		case "Coding":
			if _, ok := codingFrom(opt.Value); !ok {
				return AnswerOptionValueError{
					ItemType:  itemType,
					ValueType: valueType,
					Reason:    "valueCoding must be a Coding with a non-empty code",
				}
			}
		case "String":
			if _, ok := opt.Value.(string); !ok {
				return AnswerOptionValueError{
					ItemType:  itemType,
					ValueType: valueType,
					Reason:    "valueString must be a string",
				}
			}
		default:
			return AnswerOptionValueError{
				ItemType:  itemType,
				ValueType: valueType,
				Reason:    "expected valueCoding or valueString",
			}
		}
	}
	return nil
}

func validateAnswerOptionMarshalValue(opt AnswerOption) error {
	if opt.Value == nil {
		return nil
	}
	valueType := firstNonEmpty(opt.ValueType, opt.valueType)
	if valueType == "" {
		valueType = valueSuffix(opt.Value)
	}
	switch valueType {
	case "Coding":
		if _, ok := codingFrom(opt.Value); !ok {
			return AnswerOptionValueError{
				ValueType: valueType,
				Reason:    "valueCoding must be a Coding with a non-empty code",
			}
		}
	case "String":
		if _, ok := opt.Value.(string); !ok {
			return AnswerOptionValueError{
				ValueType: valueType,
				Reason:    "valueString must be a string",
			}
		}
	}
	return nil
}
