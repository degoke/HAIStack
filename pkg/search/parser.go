package search

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var unsupportedParams = map[string]struct{}{
	"_include":       {},
	"_revinclude":    {},
	"_summary":       {},
	"_elements":      {},
	"_contained":     {},
	"_containedType": {},
	"_filter":        {},
	"_text":          {},
	"_content":       {},
	"_list":          {},
	"_has":           {},
	"_type":          {},
}

// ParseQuery parses raw FHIR search query parameters for one resource type.
func ParseQuery(resourceType string, params url.Values) (*Query, error) {
	if resourceType == "" {
		return nil, fmt.Errorf("%w: resource type required", ErrInvalidQuery)
	}
	if params == nil {
		params = url.Values{}
	}

	q := &Query{
		ResourceType: resourceType,
		Count:        defaultCount,
	}

	for key, values := range params {
		if len(values) == 0 {
			continue
		}
		baseKey, modifier, hasModifier := splitParamKey(key)
		if hasModifier {
			return nil, fmt.Errorf("%w: modifiers not supported for %q", ErrUnsupportedFeature, key)
		}
		if strings.Contains(baseKey, ".") && !isSpecialParam(baseKey) {
			return nil, fmt.Errorf("%w: chained search not supported for %q", ErrUnsupportedFeature, key)
		}
		if _, unsupported := unsupportedParams[baseKey]; unsupported {
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedFeature, baseKey)
		}
		if modifier != "" {
			return nil, fmt.Errorf("%w: modifiers not supported for %q", ErrUnsupportedFeature, key)
		}

		switch baseKey {
		case "_count":
			if len(values) != 1 {
				return nil, fmt.Errorf("%w: _count", ErrInvalidQuery)
			}
			count, err := strconv.Atoi(values[0])
			if err != nil || count < 0 {
				return nil, fmt.Errorf("%w: _count must be a non-negative integer", ErrInvalidQuery)
			}
			if count > maxCount {
				count = maxCount
			}
			q.Count = count
			continue
		case "_offset":
			if len(values) != 1 {
				return nil, fmt.Errorf("%w: _offset", ErrInvalidQuery)
			}
			offset, err := strconv.Atoi(values[0])
			if err != nil || offset < 0 {
				return nil, fmt.Errorf("%w: _offset must be a non-negative integer", ErrInvalidQuery)
			}
			q.Offset = offset
			continue
		case "_sort":
			sortFields, err := parseSortValues(values)
			if err != nil {
				return nil, err
			}
			q.Sort = sortFields
			continue
		}

		for _, rawValue := range values {
			var orValues []string
			for _, part := range strings.Split(rawValue, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if strings.ContainsAny(part, ":|") && baseKey != "identifier" && baseKey != "code" && baseKey != "patient" && baseKey != "subject" && baseKey != "encounter" {
					if looksLikePrefix(part) {
						return nil, fmt.Errorf("%w: prefixes not supported for %q", ErrUnsupportedFeature, baseKey)
					}
				}
				orValues = append(orValues, part)
			}
			if len(orValues) == 0 {
				continue
			}
			q.Params = append(q.Params, ParamClause{
				Code:   baseKey,
				Values: orValues,
			})
		}
	}

	return q, nil
}

const (
	defaultCount = 20
	maxCount     = 100
)

func isSpecialParam(code string) bool {
	return code == "_lastUpdated"
}

func splitParamKey(key string) (base, modifier string, hasModifier bool) {
	if i := strings.Index(key, ":"); i >= 0 {
		return key[:i], key[i+1:], true
	}
	return key, "", false
}

func looksLikePrefix(value string) bool {
	prefixes := []string{"eq", "ne", "gt", "lt", "ge", "le", "sa", "eb", "ap", "co", "sw", "ew", "in", "not-in"}
	for _, p := range prefixes {
		if strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

func parseSortValues(values []string) ([]SortField, error) {
	var out []SortField
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			dir := SortAsc
			code := part
			if strings.HasPrefix(part, "-") {
				dir = SortDesc
				code = strings.TrimPrefix(part, "-")
			}
			switch code {
			case "_id", "_lastUpdated":
				out = append(out, SortField{Code: code, Direction: dir})
			default:
				return nil, fmt.Errorf("%w: sort on %q", ErrUnsupportedFeature, code)
			}
		}
	}
	return out, nil
}
