package search

import (
	"fmt"
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

	for _, clause := range q.Params {
		switch clause.Code {
		case "_id", "_lastUpdated":
			// handled below
		default:
			if !isSupportedParam(clause.Code) {
				if reg.HasSearchParameter(q.ResourceType, clause.Code) {
					return nil, fmt.Errorf("%w: %q", ErrUnsupportedParam, clause.Code)
				}
				return nil, fmt.Errorf("%w: %q", ErrUnknownParam, clause.Code)
			}
		}

		info, ok := reg.SearchParameter(q.ResourceType, clause.Code)
		if !ok && clause.Code != "_id" && clause.Code != "_lastUpdated" {
			return nil, fmt.Errorf("%w: %q", ErrUnknownParam, clause.Code)
		}
		paramType := info.Type
		if clause.Code == "_id" {
			paramType = "token"
		}
		if clause.Code == "_lastUpdated" {
			paramType = "date"
		}
		fieldKey := fieldKeyForParam(clause.Code, paramType)
		if fieldKey == "" {
			return nil, fmt.Errorf("%w: %q type %q", ErrUnsupportedParam, clause.Code, paramType)
		}
		resolved.Params = append(resolved.Params, ParamClause{
			Code:     clause.Code,
			FieldKey: fieldKey,
			Values:   clause.Values,
		})
	}
	return &resolved, nil
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
	}
	if plan.Count <= 0 {
		plan.Count = defaultCount
	}
	if len(plan.Sort) == 0 {
		plan.Sort = []SortField{{Code: "_id", Direction: SortAsc}}
	}

	for _, clause := range q.Params {
		if len(clause.Values) == 0 {
			continue
		}
		pp := ParamPlan{
			Code:        clause.Code,
			CombineMode: combineOr,
		}
		for _, value := range clause.Values {
			pp.Predicates = append(pp.Predicates, Predicate{
				FieldKey: clause.FieldKey,
				Value:    normalizeQueryValue(clause.Code, clause.FieldKey, value),
			})
		}
		plan.ParamPlans = append(plan.ParamPlans, pp)
	}
	return plan, nil
}

func normalizeQueryValue(code, fieldKey, value string) string {
	switch code {
	case "patient", "subject", "encounter":
		return normalizeReferenceString(value)
	default:
		return value
	}
}
