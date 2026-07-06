package search

import (
	"fmt"
	"strings"
)

// ResolveQuery maps parsed parameters to registry metadata and typed field keys.
func ResolveQuery(reg Registry, q *Query) (*Query, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: query is nil", ErrInvalidQuery)
	}
	if reg == nil {
		return nil, fmt.Errorf("search: registry is required")
	}
	if !reg.IsResourceEnabled(q.ResourceType) {
		return nil, ErrResourceTypeDisabled
	}

	resolved := *q
	resolved.Params = nil
	resolved.Chains = nil
	resolved.Includes = nil
	resolved.RevIncludes = nil
	resolved.Sort = nil

	for _, clause := range q.Params {
		resolvedClause, err := resolveParamClause(reg, q.ResourceType, clause)
		if err != nil {
			return nil, err
		}
		resolved.Params = append(resolved.Params, resolvedClause)
	}

	for _, chain := range q.Chains {
		resolvedChain, err := resolveChainClause(reg, q.ResourceType, chain)
		if err != nil {
			return nil, err
		}
		resolved.Chains = append(resolved.Chains, resolvedChain)
	}

	for _, inc := range q.Includes {
		resolvedInc, err := resolveInclude(reg, q.ResourceType, inc)
		if err != nil {
			return nil, err
		}
		resolved.Includes = append(resolved.Includes, resolvedInc)
	}

	for _, rev := range q.RevIncludes {
		resolvedRev, err := resolveRevInclude(reg, q.ResourceType, rev)
		if err != nil {
			return nil, err
		}
		resolved.RevIncludes = append(resolved.RevIncludes, resolvedRev)
	}

	for _, sortField := range q.Sort {
		resolvedSort, err := resolveSortField(reg, q.ResourceType, sortField)
		if err != nil {
			return nil, err
		}
		resolved.Sort = append(resolved.Sort, resolvedSort)
	}

	if q.FullText != "" {
		if q.Summary == SummaryCount {
			return nil, fmt.Errorf("%w: full-text with _summary=count", ErrInvalidQuery)
		}
	}

	return &resolved, nil
}

func resolveParamClause(reg Registry, resourceType string, clause ParamClause) (ParamClause, error) {
	info, err := lookupParam(reg, resourceType, clause.Code)
	if err != nil {
		return ParamClause{}, err
	}
	if _, err := validateModifier(info.Type, clause.Modifier); err != nil {
		return ParamClause{}, err
	}
	if info.Type == "composite" {
		return resolveCompositeClause(reg, resourceType, info, clause)
	}

	fieldKey := fieldKeyForParam(clause.Code, info.Type)
	if fieldKey == "" {
		return ParamClause{}, fmt.Errorf("%w: %q type %q", ErrUnsupportedParam, clause.Code, info.Type)
	}

	var values []ValueClause
	for _, v := range clause.Values {
		parsed, err := parseValuePrefix(info.Type, v.Raw)
		if err != nil {
			return ParamClause{}, err
		}
		values = append(values, parsed)
	}

	return ParamClause{
		Code:      clause.Code,
		Modifier:  clause.Modifier,
		ParamType: info.Type,
		FieldKey:  fieldKey,
		Values:    values,
	}, nil
}

func resolveCompositeClause(reg Registry, resourceType string, info ParameterInfo, clause ParamClause) (ParamClause, error) {
	if clause.Modifier != "" {
		return ParamClause{}, fmt.Errorf("%w: modifier on composite %q", ErrUnsupportedFeature, clause.Code)
	}
	if len(info.Component) == 0 {
		return ParamClause{}, fmt.Errorf("%w: composite %q has no components", ErrInvalidQuery, clause.Code)
	}
	for _, comp := range info.Component {
		if comp.Code == "" {
			return ParamClause{}, fmt.Errorf("%w: unresolved composite component for %q", ErrInvalidQuery, clause.Code)
		}
	}

	var values []ValueClause
	for _, v := range clause.Values {
		components, err := parseCompositeValues(v.Raw, len(info.Component))
		if err != nil {
			return ParamClause{}, err
		}
		values = append(values, ValueClause{
			Raw:      compositeIndexValue(components),
			Operator: OpEqual,
		})
	}

	return ParamClause{
		Code:      clause.Code,
		Modifier:  clause.Modifier,
		ParamType: "composite",
		FieldKey:  compositeFieldKey(clause.Code),
		Values:    values,
	}, nil
}

func resolveChainClause(reg Registry, resourceType string, chain ChainClause) (ChainClause, error) {
	refInfo, err := lookupParam(reg, resourceType, chain.RefCode)
	if err != nil {
		return ChainClause{}, err
	}
	if refInfo.Type != "reference" {
		return ChainClause{}, fmt.Errorf("%w: chain left-hand %q is not a reference", ErrInvalidQuery, chain.RefCode)
	}
	targetType, err := inferChainTargetType(refInfo, chain.Param.Code)
	if err != nil {
		return ChainClause{}, err
	}
	if !reg.IsResourceEnabled(targetType) {
		return ChainClause{}, ErrResourceTypeDisabled
	}

	resolvedParam, err := resolveParamClause(reg, targetType, chain.Param)
	if err != nil {
		return ChainClause{}, err
	}

	return ChainClause{
		RefCode:     chain.RefCode,
		RefFieldKey: fieldKeyForParam(chain.RefCode, "reference"),
		TargetType:  targetType,
		Param:       resolvedParam,
	}, nil
}

func inferChainTargetType(refInfo ParameterInfo, chainedCode string) (string, error) {
	if len(refInfo.Target) == 1 {
		return refInfo.Target[0], nil
	}
	if len(refInfo.Target) == 0 {
		return "", fmt.Errorf("%w: reference %q has no target types", ErrInvalidQuery, refInfo.Code)
	}
	for _, target := range refInfo.Target {
		if strings.EqualFold(chainedCode, strings.ToLower(target)) {
			return target, nil
		}
	}
	return refInfo.Target[0], nil
}

func resolveInclude(reg Registry, resourceType string, inc IncludeDirective) (IncludeDirective, error) {
	info, err := lookupParam(reg, resourceType, inc.ParamCode)
	if err != nil {
		return IncludeDirective{}, err
	}
	if info.Type != "reference" {
		return IncludeDirective{}, fmt.Errorf("%w: _include param %q is not a reference", ErrInvalidQuery, inc.ParamCode)
	}
	targetType := inc.TargetType
	if targetType == "" && len(info.Target) == 1 {
		targetType = info.Target[0]
	}
	if len(info.Target) > 1 {
		// Multi-target references are resolved at expansion time.
		targetType = ""
	}
	if len(info.Target) == 0 {
		return IncludeDirective{}, fmt.Errorf("%w: _include param %q has no target types", ErrInvalidQuery, inc.ParamCode)
	}
	if targetType != "" && !reg.IsResourceEnabled(targetType) {
		return IncludeDirective{}, ErrResourceTypeDisabled
	}
	return IncludeDirective{
		SourceType: resourceType,
		ParamCode:  inc.ParamCode,
		TargetType: targetType,
	}, nil
}

func resolveRevInclude(reg Registry, targetType string, rev RevIncludeDirective) (RevIncludeDirective, error) {
	if !reg.IsResourceEnabled(rev.SourceType) {
		return RevIncludeDirective{}, ErrResourceTypeDisabled
	}
	info, ok := reg.SearchParameter(rev.SourceType, rev.ParamCode)
	if !ok {
		return RevIncludeDirective{}, fmt.Errorf("%w: %q on %s", ErrUnknownParam, rev.ParamCode, rev.SourceType)
	}
	if info.Type != "reference" {
		return RevIncludeDirective{}, fmt.Errorf("%w: _revinclude param %q is not a reference", ErrInvalidQuery, rev.ParamCode)
	}
	validTarget := false
	for _, t := range info.Target {
		if t == targetType {
			validTarget = true
			break
		}
	}
	if !validTarget && len(info.Target) > 0 {
		return RevIncludeDirective{}, fmt.Errorf("%w: _revinclude %s:%s does not reference %s", ErrInvalidQuery, rev.SourceType, rev.ParamCode, targetType)
	}
	return RevIncludeDirective{
		SourceType: rev.SourceType,
		ParamCode:  rev.ParamCode,
		TargetType: targetType,
	}, nil
}

func resolveSortField(reg Registry, resourceType string, field SortField) (SortField, error) {
	switch field.Code {
	case "_id":
		return SortField{Code: "_id", FieldKey: "token._id", Direction: field.Direction}, nil
	case "_lastUpdated":
		return SortField{Code: "_lastUpdated", FieldKey: "date._lastUpdated", Direction: field.Direction}, nil
	}
	info, err := lookupParam(reg, resourceType, field.Code)
	if err != nil {
		return SortField{}, err
	}
	fieldKey := fieldKeyForParam(field.Code, info.Type)
	if fieldKey == "" {
		return SortField{}, fmt.Errorf("%w: sort on %q type %q", ErrUnsupportedFeature, field.Code, info.Type)
	}
	return SortField{Code: field.Code, FieldKey: fieldKey, Direction: field.Direction}, nil
}

func lookupParam(reg Registry, resourceType, code string) (ParameterInfo, error) {
	switch code {
	case "_id":
		return ParameterInfo{Code: "_id", Type: "token"}, nil
	case "_lastUpdated":
		return ParameterInfo{Code: "_lastUpdated", Type: "date"}, nil
	}
	info, ok := reg.SearchParameter(resourceType, code)
	if !ok {
		return ParameterInfo{}, fmt.Errorf("%w: %q", ErrUnknownParam, code)
	}
	if !isSearchableType(info.Type) {
		return ParameterInfo{}, fmt.Errorf("%w: %q type %q", ErrUnsupportedParam, code, info.Type)
	}
	return info, nil
}

// BuildPlan converts a resolved query into a backend-neutral execution plan.
func BuildPlan(q *Query) (*Plan, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: query is nil", ErrInvalidQuery)
	}
	plan := &Plan{
		ResourceType: q.ResourceType,
		Count:        q.Count,
		Offset:       q.Offset,
		Sort:         q.Sort,
		Summary:      q.Summary,
		Elements:     append([]string(nil), q.Elements...),
		FullText:     q.FullText,
	}
	if plan.Count <= 0 {
		plan.Count = defaultCount
	}
	if len(plan.Sort) == 0 {
		plan.Sort = []SortField{{Code: "_id", FieldKey: "token._id", Direction: SortAsc}}
	}

	for _, clause := range q.Params {
		pp, err := buildParamPlan(q.ResourceType, clause)
		if err != nil {
			return nil, err
		}
		if pp != nil {
			plan.ParamPlans = append(plan.ParamPlans, *pp)
		}
	}

	for _, chain := range q.Chains {
		pp, err := buildParamPlan(chain.TargetType, chain.Param)
		if err != nil {
			return nil, err
		}
		plan.ChainPlans = append(plan.ChainPlans, ChainPlan{
			RefCode:     chain.RefCode,
			RefFieldKey: chain.RefFieldKey,
			TargetType:  chain.TargetType,
			ParamPlan:   *pp,
		})
	}

	for _, inc := range q.Includes {
		plan.Includes = append(plan.Includes, IncludePlan{
			SourceType:  inc.SourceType,
			ParamCode:   inc.ParamCode,
			RefFieldKey: fieldKeyForParam(inc.ParamCode, "reference"),
			TargetType:  inc.TargetType,
		})
	}

	for _, rev := range q.RevIncludes {
		plan.RevIncludes = append(plan.RevIncludes, RevIncludePlan{
			SourceType:  rev.SourceType,
			ParamCode:   rev.ParamCode,
			RefFieldKey: fieldKeyForParam(rev.ParamCode, "reference"),
			TargetType:  rev.TargetType,
		})
	}

	return plan, nil
}

func buildParamPlan(resourceType string, clause ParamClause) (*ParamPlan, error) {
	if len(clause.Values) == 0 {
		return nil, nil
	}
	pp := ParamPlan{
		Code:        clause.Code,
		FieldKey:    clause.FieldKey,
		ParamType:   clause.ParamType,
		CombineMode: combineOr,
	}
	for _, value := range clause.Values {
		op := value.Operator
		if op == OpEqual || op == "" {
			if clause.Modifier != "" {
				validated, err := validateModifier(clause.ParamType, clause.Modifier)
				if err != nil {
					return nil, err
				}
				op = validated
			}
		}
		pp.Predicates = append(pp.Predicates, Predicate{
			FieldKey: clause.FieldKey,
			Value:    normalizeQueryValue(clause.Code, clause.FieldKey, value.Raw, op),
			Operator: op,
		})
	}
	return &pp, nil
}

func normalizeQueryValue(code, fieldKey, value string, op MatchOperator) string {
	switch {
	case strings.HasPrefix(fieldKey, "reference."):
		switch op {
		case OpIdentifier:
			return normalizeReferenceString(value)
		case OpType:
			return value
		default:
			return normalizeReferenceString(value)
		}
	case strings.HasPrefix(fieldKey, "composite."):
		return value
	default:
		return value
	}
}

func compositeFieldKey(code string) string {
	return "composite." + code
}

func compositeIndexValue(components []string) string {
	return strings.Join(components, "$")
}
