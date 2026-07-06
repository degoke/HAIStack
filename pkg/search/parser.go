package search

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

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

		if _, deferred := deferredParams[baseKey]; deferred {
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedFeature, baseKey)
		}

		switch baseKey {
		case "_count":
			if err := parseCount(values, q); err != nil {
				return nil, err
			}
			continue
		case "_offset":
			if err := parseOffset(values, q); err != nil {
				return nil, err
			}
			continue
		case "_sort":
			sortFields, err := parseSortValues(values)
			if err != nil {
				return nil, err
			}
			q.Sort = sortFields
			continue
		case "_include":
			for _, raw := range values {
				directive, err := parseIncludeValue(resourceType, raw)
				if err != nil {
					return nil, err
				}
				q.Includes = append(q.Includes, directive)
			}
			continue
		case "_revinclude":
			for _, raw := range values {
				directive, err := parseRevIncludeValue(resourceType, raw)
				if err != nil {
					return nil, err
				}
				q.RevIncludes = append(q.RevIncludes, directive)
			}
			continue
		case "_summary":
			if len(values) != 1 {
				return nil, fmt.Errorf("%w: _summary", ErrInvalidQuery)
			}
			mode := SummaryMode(values[0])
			switch mode {
			case SummaryTrue, SummaryText, SummaryData, SummaryCount:
				q.Summary = mode
			default:
				return nil, fmt.Errorf("%w: _summary=%q", ErrUnsupportedFeature, values[0])
			}
			continue
		case "_elements":
			for _, raw := range values {
				for _, part := range strings.Split(raw, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					q.Elements = append(q.Elements, part)
				}
			}
			continue
		case "_text", "_content":
			if len(values) != 1 {
				return nil, fmt.Errorf("%w: %s", ErrInvalidQuery, baseKey)
			}
			if q.FullText != "" {
				return nil, fmt.Errorf("%w: only one full-text parameter allowed", ErrInvalidQuery)
			}
			q.FullText = values[0]
			continue
		}

		if strings.Contains(baseKey, ".") && !isSpecialParam(baseKey) {
			chain, err := parseChainKey(baseKey, modifier, hasModifier, values)
			if err != nil {
				return nil, err
			}
			q.Chains = append(q.Chains, chain)
			continue
		}

		if hasModifier && modifier == "missing" {
			return nil, fmt.Errorf("%w: modifier %q", ErrUnsupportedFeature, modifier)
		}

		for _, rawValue := range values {
			orValues, err := splitORValues(rawValue)
			if err != nil {
				return nil, err
			}
			if len(orValues) == 0 {
				continue
			}
			clause := ParamClause{
				Code:     baseKey,
				Modifier: modifier,
				Values:   orValues,
			}
			q.Params = append(q.Params, clause)
		}
	}

	if q.Summary == SummaryCount && len(q.Elements) > 0 {
		return nil, fmt.Errorf("%w: _summary=count with _elements", ErrInvalidQuery)
	}
	return q, nil
}

const (
	defaultCount = 20
	maxCount     = 100
)

func parseCount(values []string, q *Query) error {
	if len(values) != 1 {
		return fmt.Errorf("%w: _count", ErrInvalidQuery)
	}
	count, err := strconv.Atoi(values[0])
	if err != nil || count < 0 {
		return fmt.Errorf("%w: _count must be a non-negative integer", ErrInvalidQuery)
	}
	if count > maxCount {
		count = maxCount
	}
	q.Count = count
	return nil
}

func parseOffset(values []string, q *Query) error {
	if len(values) != 1 {
		return fmt.Errorf("%w: _offset", ErrInvalidQuery)
	}
	offset, err := strconv.Atoi(values[0])
	if err != nil || offset < 0 {
		return fmt.Errorf("%w: _offset must be a non-negative integer", ErrInvalidQuery)
	}
	q.Offset = offset
	return nil
}

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

func splitORValues(rawValue string) ([]ValueClause, error) {
	var out []ValueClause
	for _, part := range strings.Split(rawValue, ",") {
		part = trimValue(part)
		if part == "" {
			continue
		}
		out = append(out, ValueClause{Raw: part, Operator: OpEqual})
	}
	return out, nil
}

func parseChainKey(key, modifier string, hasModifier bool, values []string) (ChainClause, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return ChainClause{}, fmt.Errorf("%w: chained search %q", ErrUnsupportedFeature, key)
	}
	if strings.Contains(parts[1], ".") {
		return ChainClause{}, fmt.Errorf("%w: chain depth > 1 for %q", ErrUnsupportedFeature, key)
	}
	if hasModifier && modifier == "missing" {
		return ChainClause{}, fmt.Errorf("%w: modifier %q on chain", ErrUnsupportedFeature, modifier)
	}
	var valueClauses []ValueClause
	for _, rawValue := range values {
		orValues, err := splitORValues(rawValue)
		if err != nil {
			return ChainClause{}, err
		}
		valueClauses = append(valueClauses, orValues...)
	}
	return ChainClause{
		RefCode: parts[0],
		Param: ParamClause{
			Code:     parts[1],
			Modifier: modifier,
			Values:   valueClauses,
		},
	}, nil
}

func parseIncludeValue(sourceType, raw string) (IncludeDirective, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return IncludeDirective{}, fmt.Errorf("%w: _include %q", ErrInvalidQuery, raw)
	}
	if parts[0] == "*" || parts[1] == "*" {
		return IncludeDirective{}, fmt.Errorf("%w: wildcard _include", ErrUnsupportedFeature)
	}
	if parts[0] != sourceType {
		return IncludeDirective{}, fmt.Errorf("%w: _include source type %q does not match search type %q", ErrInvalidQuery, parts[0], sourceType)
	}
	return IncludeDirective{
		SourceType: parts[0],
		ParamCode:  parts[1],
	}, nil
}

func parseRevIncludeValue(searchType, raw string) (RevIncludeDirective, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return RevIncludeDirective{}, fmt.Errorf("%w: _revinclude %q", ErrInvalidQuery, raw)
	}
	if parts[0] == "*" || parts[1] == "*" {
		return RevIncludeDirective{}, fmt.Errorf("%w: wildcard _revinclude", ErrUnsupportedFeature)
	}
	return RevIncludeDirective{
		SourceType: parts[0],
		ParamCode:  parts[1],
		TargetType: searchType,
	}, nil
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
			out = append(out, SortField{Code: code, Direction: dir})
		}
	}
	return out, nil
}
