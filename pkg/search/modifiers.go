package search

import (
	"fmt"
	"strings"
)

var deferredParams = map[string]struct{}{
	"_contained":     {},
	"_containedType": {},
	"_filter":        {},
	"_list":          {},
	"_has":           {},
	"_type":          {},
}

var stringModifiers = map[string]MatchOperator{
	"exact":    OpExact,
	"contains": OpContains,
}

var tokenModifiers = map[string]MatchOperator{
	"text": OpText,
	"not":  OpNot,
}

var referenceModifiers = map[string]MatchOperator{
	"identifier": OpIdentifier,
	"type":       OpType,
}

var dateNumberPrefixes = map[string]MatchOperator{
	"eq": OpEqual,
	"ne": OpNotEqual,
	"gt": OpGreater,
	"lt": OpLess,
	"ge": OpGE,
	"le": OpLE,
	"sa": OpStarts,
	"eb": OpEnds,
	"ap": OpApprox,
}

func validateModifier(paramType, modifier string) (MatchOperator, error) {
	if modifier == "" {
		return OpEqual, nil
	}
	switch paramType {
	case "string":
		if op, ok := stringModifiers[modifier]; ok {
			return op, nil
		}
	case "token":
		if op, ok := tokenModifiers[modifier]; ok {
			return op, nil
		}
	case "reference":
		if op, ok := referenceModifiers[modifier]; ok {
			return op, nil
		}
	case "uri":
		return "", fmt.Errorf("%w: modifier %q on type %q", ErrUnsupportedFeature, modifier, paramType)
	}
	return "", fmt.Errorf("%w: modifier %q on type %q", ErrUnsupportedFeature, modifier, paramType)
}

func parseValuePrefix(paramType, raw string) (ValueClause, error) {
	raw = trimValue(raw)
	if raw == "" {
		return ValueClause{}, fmt.Errorf("%w: empty value", ErrInvalidQuery)
	}
	switch paramType {
	case "date", "number", "quantity":
		for _, prefix := range []string{"not-in", "not"} {
			if len(raw) > len(prefix) && raw[:len(prefix)] == prefix {
				return ValueClause{}, fmt.Errorf("%w: prefix %q on type %q", ErrUnsupportedFeature, prefix, paramType)
			}
		}
		for _, prefix := range []string{"eq", "ne", "gt", "lt", "ge", "le", "sa", "eb", "ap"} {
			if len(raw) > len(prefix) && raw[:len(prefix)] == prefix {
				op, ok := dateNumberPrefixes[prefix]
				if !ok {
					continue
				}
				return ValueClause{
					Raw:      raw[len(prefix):],
					Prefix:   prefix,
					Operator: op,
				}, nil
			}
		}
		return ValueClause{Raw: raw, Operator: OpEqual}, nil
	case "token", "reference":
		if looksLikePrefix(raw) && !isTokenLiteral(raw) {
			return ValueClause{}, fmt.Errorf("%w: prefixes not supported for %q", ErrUnsupportedFeature, paramType)
		}
		return ValueClause{Raw: raw, Operator: OpEqual}, nil
	default:
		if looksLikePrefix(raw) {
			return ValueClause{}, fmt.Errorf("%w: prefixes not supported for %q", ErrUnsupportedFeature, paramType)
		}
		return ValueClause{Raw: raw, Operator: OpEqual}, nil
	}
}

func isTokenLiteral(raw string) bool {
	return stringsContainsPipeOrColon(raw)
}

func stringsContainsPipeOrColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' || s[i] == ':' {
			return true
		}
	}
	return false
}

func trimValue(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func parseCompositeValues(raw string, componentCount int) ([]string, error) {
	if componentCount <= 0 {
		return nil, fmt.Errorf("%w: composite parameter has no components", ErrInvalidQuery)
	}
	parts := splitCompositeValue(raw)
	if len(parts) != componentCount {
		return nil, fmt.Errorf("%w: composite value has %d components, want %d", ErrInvalidQuery, len(parts), componentCount)
	}
	return parts, nil
}

func splitCompositeValue(raw string) []string {
	var parts []string
	var current strings.Builder
	escaped := false
	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '$' {
			parts = append(parts, trimValue(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	parts = append(parts, trimValue(current.String()))
	if len(parts) == 0 {
		return nil
	}
	return parts
}