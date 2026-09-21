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
	resolved.Has = nil
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

	for _, has := range q.Has {
		resolvedHas, err := resolveHasClause(reg, q.ResourceType, has)
		if err != nil {
			return nil, err
		}
		resolved.Has = append(resolved.Has, resolvedHas)
	}

	for _, inc := range q.Includes {
		expanded, err := resolveIncludes(reg, q.ResourceType, inc)
		if err != nil {
			return nil, err
		}
		resolved.Includes = append(resolved.Includes, expanded...)
	}

	for _, rev := range q.RevIncludes {
		expanded, err := resolveRevIncludes(reg, q.ResourceType, rev)
		if err != nil {
			return nil, err
		}
		resolved.RevIncludes = append(resolved.RevIncludes, expanded...)
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

	if chain.Nested != nil {
		targetType, err := inferChainTargetType(reg, refInfo, chain.Nested.RefCode)
		if err != nil {
			return ChainClause{}, err
		}
		if !reg.IsResourceEnabled(targetType) {
			return ChainClause{}, ErrResourceTypeDisabled
		}
		nested, err := resolveChainClause(reg, targetType, *chain.Nested)
		if err != nil {
			return ChainClause{}, err
		}
		return ChainClause{
			RefCode:     chain.RefCode,
			RefFieldKey: fieldKeyForParam(chain.RefCode, "reference"),
			TargetType:  targetType,
			Nested:      &nested,
		}, nil
	}

	targetType, err := inferChainTargetType(reg, refInfo, chain.Param.Code)
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

func resolveHasClause(reg Registry, searchType string, has HasClause) (HasClause, error) {
	if !reg.IsResourceEnabled(has.SourceType) {
		return HasClause{}, ErrResourceTypeDisabled
	}
	refInfo, err := lookupParam(reg, has.SourceType, has.RefCode)
	if err != nil {
		return HasClause{}, err
	}
	if refInfo.Type != "reference" {
		return HasClause{}, fmt.Errorf("%w: _has reference %q is not a reference", ErrInvalidQuery, has.RefCode)
	}
	if err := validateHasTarget(refInfo, searchType); err != nil {
		return HasClause{}, err
	}

	resolved := HasClause{
		SourceType:  has.SourceType,
		RefCode:     has.RefCode,
		RefFieldKey: fieldKeyForParam(has.RefCode, "reference"),
	}

	switch {
	case has.Nested != nil:
		nested, err := resolveHasClause(reg, has.SourceType, *has.Nested)
		if err != nil {
			return HasClause{}, err
		}
		resolved.Nested = &nested
	case has.Chain != nil:
		chain, err := resolveChainClause(reg, has.SourceType, *has.Chain)
		if err != nil {
			return HasClause{}, err
		}
		resolved.Chain = &chain
	default:
		param, err := resolveParamClause(reg, has.SourceType, has.Param)
		if err != nil {
			return HasClause{}, err
		}
		resolved.Param = param
	}
	return resolved, nil
}

func validateHasTarget(refInfo ParameterInfo, searchType string) error {
	if len(refInfo.Target) == 0 {
		return nil
	}
	for _, target := range refInfo.Target {
		if target == searchType {
			return nil
		}
	}
	return fmt.Errorf("%w: _has reference %q does not target %s", ErrInvalidQuery, refInfo.Code, searchType)
}

func inferChainTargetType(reg Registry, refInfo ParameterInfo, chainedCode string) (string, error) {
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
	if reg != nil {
		for _, target := range refInfo.Target {
			if reg.IsResourceEnabled(target) && reg.HasSearchParameter(target, chainedCode) {
				return target, nil
			}
		}
		for _, target := range refInfo.Target {
			if reg.IsResourceEnabled(target) {
				return target, nil
			}
		}
	}
	return refInfo.Target[0], nil
}

// maxWildcardIncludeExpansion is the maximum number of concrete include/revinclude
// directives produced from one _include/_revinclude value, including *:* wildcards.
// Larger expansions are rejected rather than fanning out unbounded reference parameters.
const maxWildcardIncludeExpansion = 64

func resolveIncludes(reg Registry, resourceType string, inc IncludeDirective) ([]IncludeDirective, error) {
	if inc.ParamCode != "*" {
		resolved, err := resolveInclude(reg, resourceType, inc)
		if err != nil {
			return nil, err
		}
		if err := checkIncludeExpansionLimit("_include", len(resolved)); err != nil {
			return nil, err
		}
		return resolved, nil
	}
	var out []IncludeDirective
	for _, info := range reg.SearchParametersFor(resourceType) {
		if info.Type != "reference" {
			continue
		}
		if inc.TargetType != "" && inc.TargetType != "*" && !paramTargets(info, inc.TargetType) {
			continue
		}
		resolved, err := resolveInclude(reg, resourceType, IncludeDirective{
			SourceType: resourceType,
			ParamCode:  info.Code,
			TargetType: inc.TargetType,
		})
		if err != nil {
			continue
		}
		out = append(out, resolved...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: _include %s:* matched no reference parameters", ErrInvalidQuery, resourceType)
	}
	if err := checkIncludeExpansionLimit("_include", len(out)); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveRevIncludes(reg Registry, targetType string, rev RevIncludeDirective) ([]RevIncludeDirective, error) {
	if rev.SourceType != "*" && rev.ParamCode != "*" {
		resolved, err := resolveRevInclude(reg, targetType, rev)
		if err != nil {
			return nil, err
		}
		return []RevIncludeDirective{resolved}, nil
	}

	sourceTypes := []string{rev.SourceType}
	if rev.SourceType == "*" {
		sourceTypes = reg.EnabledResourceTypes()
	}

	var out []RevIncludeDirective
	for _, sourceType := range sourceTypes {
		if !reg.IsResourceEnabled(sourceType) {
			continue
		}
		for _, info := range reg.SearchParametersFor(sourceType) {
			if info.Type != "reference" {
				continue
			}
			if rev.ParamCode != "*" && info.Code != rev.ParamCode {
				continue
			}
			if !paramTargets(info, targetType) && len(info.Target) > 0 {
				continue
			}
			resolved, err := resolveRevInclude(reg, targetType, RevIncludeDirective{
				SourceType: sourceType,
				ParamCode:  info.Code,
				TargetType: targetType,
			})
			if err != nil {
				continue
			}
			out = append(out, resolved)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: _revinclude %s:%s matched no reference parameters", ErrInvalidQuery, rev.SourceType, rev.ParamCode)
	}
	if err := checkIncludeExpansionLimit("_revinclude", len(out)); err != nil {
		return nil, err
	}
	return out, nil
}

func checkIncludeExpansionLimit(kind string, n int) error {
	if n > maxWildcardIncludeExpansion {
		return fmt.Errorf("%w: %s expanded to %d directives (max %d)", ErrInvalidQuery, kind, n, maxWildcardIncludeExpansion)
	}
	return nil
}

func paramTargets(info ParameterInfo, targetType string) bool {
	if targetType == "" || targetType == "*" {
		return true
	}
	if len(info.Target) == 0 {
		return true
	}
	for _, t := range info.Target {
		if t == targetType {
			return true
		}
	}
	return false
}

func resolveInclude(reg Registry, resourceType string, inc IncludeDirective) ([]IncludeDirective, error) {
	info, err := lookupParam(reg, resourceType, inc.ParamCode)
	if err != nil {
		return nil, err
	}
	if info.Type != "reference" {
		return nil, fmt.Errorf("%w: _include param %q is not a reference", ErrInvalidQuery, inc.ParamCode)
	}
	if len(info.Target) == 0 {
		return nil, fmt.Errorf("%w: _include param %q has no target types", ErrInvalidQuery, inc.ParamCode)
	}
	targetType := inc.TargetType
	if targetType == "*" {
		targetType = ""
	}
	if targetType != "" {
		if !paramTargets(info, targetType) && len(info.Target) > 0 {
			return nil, fmt.Errorf("%w: _include param %q does not target %s", ErrInvalidQuery, inc.ParamCode, targetType)
		}
		if !reg.IsResourceEnabled(targetType) {
			return nil, ErrResourceTypeDisabled
		}
		return []IncludeDirective{{
			SourceType: resourceType,
			ParamCode:  inc.ParamCode,
			TargetType: targetType,
		}}, nil
	}

	// Multi-target references (e.g. Observation.encounter → Encounter|EpisodeOfCare)
	// expand only to enabled types so later Search reads never touch disabled types.
	var out []IncludeDirective
	for _, target := range info.Target {
		if !reg.IsResourceEnabled(target) {
			continue
		}
		out = append(out, IncludeDirective{
			SourceType: resourceType,
			ParamCode:  inc.ParamCode,
			TargetType: target,
		})
	}
	if len(out) == 0 {
		return nil, ErrResourceTypeDisabled
	}
	return out, nil
}

func resolveRevInclude(reg Registry, targetType string, rev RevIncludeDirective) (RevIncludeDirective, error) {
	if !reg.IsResourceEnabled(rev.SourceType) {
		return RevIncludeDirective{}, ErrResourceTypeDisabled
	}
	info, ok := reg.SearchParameter(rev.SourceType, rev.ParamCode)
	if !ok {
		return RevIncludeDirective{}, UnknownParamError{ResourceType: rev.SourceType, Code: rev.ParamCode}
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
		return ParameterInfo{}, UnknownParamError{ResourceType: resourceType, Code: code}
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
	if !q.CountSet {
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
		cp, err := buildChainPlan(chain)
		if err != nil {
			return nil, err
		}
		plan.ChainPlans = append(plan.ChainPlans, *cp)
	}

	for _, has := range q.Has {
		hp, err := buildHasPlan(has)
		if err != nil {
			return nil, err
		}
		plan.HasPlans = append(plan.HasPlans, *hp)
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

func buildChainPlan(chain ChainClause) (*ChainPlan, error) {
	plan := &ChainPlan{
		RefCode:     chain.RefCode,
		RefFieldKey: chain.RefFieldKey,
		TargetType:  chain.TargetType,
	}
	if chain.Nested != nil {
		nested, err := buildChainPlan(*chain.Nested)
		if err != nil {
			return nil, err
		}
		plan.Nested = nested
		return plan, nil
	}
	pp, err := buildParamPlan(chain.TargetType, chain.Param)
	if err != nil {
		return nil, err
	}
	if pp == nil {
		return nil, fmt.Errorf("%w: chained search %q has no value", ErrInvalidQuery, chain.RefCode)
	}
	plan.ParamPlan = *pp
	return plan, nil
}

func buildHasPlan(has HasClause) (*HasPlan, error) {
	plan := &HasPlan{
		SourceType:  has.SourceType,
		RefCode:     has.RefCode,
		RefFieldKey: has.RefFieldKey,
	}
	switch {
	case has.Nested != nil:
		nested, err := buildHasPlan(*has.Nested)
		if err != nil {
			return nil, err
		}
		plan.Nested = nested
	case has.Chain != nil:
		chain, err := buildChainPlan(*has.Chain)
		if err != nil {
			return nil, err
		}
		plan.ChainPlan = chain
	default:
		pp, err := buildParamPlan(has.SourceType, has.Param)
		if err != nil {
			return nil, err
		}
		if pp == nil {
			return nil, fmt.Errorf("%w: _has %s:%s has no value", ErrInvalidQuery, has.SourceType, has.RefCode)
		}
		plan.ParamPlan = *pp
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
