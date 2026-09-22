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
		case "_has":
			if !hasModifier {
				return nil, fmt.Errorf("%w: _has", ErrInvalidQuery)
			}
			hasClause, err := parseHasKey(modifier, values)
			if err != nil {
				return nil, err
			}
			q.Has = append(q.Has, hasClause)
			continue
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
			if hasModifier {
				return nil, fmt.Errorf("%w: _include modifier %q", ErrUnsupportedFeature, modifier)
			}
			for _, raw := range values {
				directive, err := parseIncludeValue(resourceType, raw)
				if err != nil {
					return nil, err
				}
				q.Includes = append(q.Includes, directive)
			}
			continue
		case "_revinclude":
			if hasModifier {
				return nil, fmt.Errorf("%w: _revinclude modifier %q", ErrUnsupportedFeature, modifier)
			}
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
			case SummaryFalse, SummaryTrue, SummaryText, SummaryData, SummaryCount:
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
	defaultCount  = 20
	maxCount      = 100
	maxChainHops  = 2
	maxHasNesting = 2
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
	q.CountSet = true
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
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return ChainClause{}, fmt.Errorf("%w: chained search %q", ErrUnsupportedFeature, key)
	}
	hops := len(parts) - 1
	if hops > maxChainHops {
		return ChainClause{}, fmt.Errorf("%w: chain depth > %d for %q", ErrUnsupportedFeature, maxChainHops, key)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ChainClause{}, fmt.Errorf("%w: chained search %q", ErrInvalidQuery, key)
		}
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
	return buildChainClause(parts, modifier, valueClauses), nil
}

func buildChainClause(parts []string, modifier string, values []ValueClause) ChainClause {
	if len(parts) == 2 {
		return ChainClause{
			RefCode: parts[0],
			Param: ParamClause{
				Code:     parts[1],
				Modifier: modifier,
				Values:   values,
			},
		}
	}
	nested := buildChainClause(parts[1:], modifier, values)
	return ChainClause{
		RefCode: parts[0],
		Nested:  &nested,
	}
}

func parseHasKey(rest string, values []string) (HasClause, error) {
	return parseHasRest(rest, values, 1)
}

func parseHasRest(rest string, values []string, depth int) (HasClause, error) {
	if depth > maxHasNesting {
		return HasClause{}, fmt.Errorf("%w: _has nesting > %d", ErrUnsupportedFeature, maxHasNesting)
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return HasClause{}, fmt.Errorf("%w: _has %q", ErrInvalidQuery, rest)
	}
	sourceType, refCode, remainder := parts[0], parts[1], parts[2]
	if strings.HasPrefix(remainder, "_has:") {
		nested, err := parseHasRest(strings.TrimPrefix(remainder, "_has:"), values, depth+1)
		if err != nil {
			return HasClause{}, err
		}
		return HasClause{
			SourceType: sourceType,
			RefCode:    refCode,
			Nested:     &nested,
		}, nil
	}

	paramKey, paramMod, hasMod := splitParamKey(remainder)
	if hasMod && paramMod == "missing" {
		return HasClause{}, fmt.Errorf("%w: modifier %q on _has", ErrUnsupportedFeature, paramMod)
	}
	if strings.Contains(paramKey, ".") {
		chain, err := parseChainKey(paramKey, paramMod, hasMod, values)
		if err != nil {
			return HasClause{}, err
		}
		return HasClause{
			SourceType: sourceType,
			RefCode:    refCode,
			Chain:      &chain,
		}, nil
	}

	var valueClauses []ValueClause
	for _, rawValue := range values {
		orValues, err := splitORValues(rawValue)
		if err != nil {
			return HasClause{}, err
		}
		valueClauses = append(valueClauses, orValues...)
	}
	return HasClause{
		SourceType: sourceType,
		RefCode:    refCode,
		Param: ParamClause{
			Code:     paramKey,
			Modifier: paramMod,
			Values:   valueClauses,
		},
	}, nil
}

func parseIncludeValue(sourceType, raw string) (IncludeDirective, error) {
	src, param, target, err := parseIncludeParts("_include", raw)
	if err != nil {
		return IncludeDirective{}, err
	}
	if src == "*" {
		src = sourceType
	}
	if src != sourceType {
		return IncludeDirective{}, fmt.Errorf("%w: _include source type %q does not match search type %q", ErrInvalidQuery, src, sourceType)
	}
	return IncludeDirective{
		SourceType: src,
		ParamCode:  param,
		TargetType: target,
	}, nil
}

func parseRevIncludeValue(searchType, raw string) (RevIncludeDirective, error) {
	src, param, target, err := parseIncludeParts("_revinclude", raw)
	if err != nil {
		return RevIncludeDirective{}, err
	}
	if target != "" && target != searchType && target != "*" {
		return RevIncludeDirective{}, fmt.Errorf("%w: _revinclude target type %q does not match search type %q", ErrInvalidQuery, target, searchType)
	}
	return RevIncludeDirective{
		SourceType: src,
		ParamCode:  param,
		TargetType: searchType,
	}, nil
}

func parseIncludeParts(kind, raw string) (source, param, target string, err error) {
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return "", "", "", fmt.Errorf("%w: %s %q", ErrInvalidQuery, kind, raw)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", "", "", fmt.Errorf("%w: %s %q", ErrInvalidQuery, kind, raw)
		}
	}
	source, param = parts[0], parts[1]
	if len(parts) == 3 {
		target = parts[2]
	}
	return source, param, target, nil
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
