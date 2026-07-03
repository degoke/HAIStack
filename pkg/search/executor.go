package search

import (
	"context"
	"fmt"
	"sort"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// Executor executes a search plan against a typed index backend.
type Executor interface {
	Execute(ctx context.Context, plan *Plan) ([]string, error)
}

// StoreExecutor executes search plans using store.SearchQueryExecutor.
type StoreExecutor struct {
	Backend   store.SearchQueryExecutor
	Resources store.ResourceStore
}

// NewStoreExecutor constructs an executor backed by SearchQueryExecutor.
func NewStoreExecutor(backend store.SearchQueryExecutor, resources store.ResourceStore) *StoreExecutor {
	return &StoreExecutor{Backend: backend, Resources: resources}
}

// Execute runs the plan and returns matching resource IDs in deterministic order.
func (e *StoreExecutor) Execute(ctx context.Context, plan *Plan) ([]string, error) {
	if e == nil || e.Backend == nil {
		return nil, fmt.Errorf("search: executor backend is required")
	}
	if plan == nil {
		return nil, fmt.Errorf("%w: plan is nil", ErrInvalidQuery)
	}

	var candidateSets [][]string
	for _, paramPlan := range plan.ParamPlans {
		ids, err := e.executeParamPlan(ctx, plan.ResourceType, paramPlan)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		candidateSets = append(candidateSets, ids)
	}

	var ids []string
	var err error
	if len(candidateSets) == 0 {
		ids, err = e.listAllResourceIDs(ctx, plan.ResourceType)
		if err != nil {
			return nil, err
		}
	} else {
		ids = intersectSorted(candidateSets)
	}

	ids, err = e.sortResourceIDs(ctx, plan.ResourceType, ids, plan.Sort)
	if err != nil {
		return nil, err
	}

	start := plan.Offset
	if start > len(ids) {
		return nil, nil
	}
	end := start + plan.Count
	if end > len(ids) {
		end = len(ids)
	}
	return ids[start:end], nil
}

func (e *StoreExecutor) listAllResourceIDs(ctx context.Context, resourceType string) ([]string, error) {
	if e.Resources == nil {
		return nil, fmt.Errorf("search: resource store is required for unpredicated search")
	}
	const batch = 500
	var all []string
	offset := 0
	for {
		ids, err := e.Resources.ListIDs(ctx, resourceType, batch, offset)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		all = append(all, ids...)
		if len(ids) < batch {
			break
		}
		offset += len(ids)
	}
	return all, nil
}

func (e *StoreExecutor) sortResourceIDs(ctx context.Context, resourceType string, ids []string, sortFields []SortField) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(sortFields) == 0 {
		sortFields = []SortField{{Code: "_id", Direction: SortAsc}}
	}

	type row struct {
		id     string
		values []string
	}
	rows := make([]row, len(ids))
	valueMaps := make([]map[string]string, len(sortFields))

	for i, field := range sortFields {
		switch field.Code {
		case "_id":
			m := make(map[string]string, len(ids))
			for _, id := range ids {
				m[id] = id
			}
			valueMaps[i] = m
		default:
			fieldKey := fieldKeyForSort(field.Code)
			if fieldKey == "" {
				return nil, fmt.Errorf("%w: sort on %q", ErrUnsupportedFeature, field.Code)
			}
			values, err := e.Backend.FieldValues(ctx, resourceType, fieldKey, ids)
			if err != nil {
				return nil, err
			}
			valueMaps[i] = values
		}
	}

	for i, id := range ids {
		rows[i].id = id
		for j := range sortFields {
			value := valueMaps[j][id]
			if value == "" && sortFields[j].Code == "_id" {
				value = id
			}
			rows[i].values = append(rows[i].values, value)
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		for k, field := range sortFields {
			left := rows[i].values[k]
			right := rows[j].values[k]
			if left == right {
				continue
			}
			if field.Direction == SortDesc {
				return left > right
			}
			return left < right
		}
		return rows[i].id < rows[j].id
	})

	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.id
	}
	return out, nil
}

func (e *StoreExecutor) executeParamPlan(ctx context.Context, resourceType string, plan ParamPlan) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	for _, pred := range plan.Predicates {
		matches, err := e.Backend.LookupMatch(ctx, store.SearchMatch{
			ResourceType: resourceType,
			FieldKey:     pred.FieldKey,
			Value:        pred.Value,
		})
		if err != nil {
			return nil, err
		}
		for _, id := range matches {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func intersectSorted(sets [][]string) []string {
	if len(sets) == 0 {
		return nil
	}
	if len(sets) == 1 {
		out := append([]string(nil), sets[0]...)
		sort.Strings(out)
		return out
	}
	current := sets[0]
	for _, next := range sets[1:] {
		current = intersectTwo(current, next)
		if len(current) == 0 {
			return nil
		}
	}
	return current
}

func intersectTwo(a, b []string) []string {
	i, j := 0, 0
	var out []string
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}
