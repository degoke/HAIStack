package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/degoke/haistack/pkg/store"
)

// Executor executes a search plan against a typed index backend.
type Executor interface {
	Execute(ctx context.Context, plan *Plan) (*ExecuteResult, error)
}

// ExecuteResult holds primary match IDs, total count, optional full-text scores, and includes.
type ExecuteResult struct {
	IDs      []string
	Total    int
	Scores   map[string]float64
	Included []IncludedResource
}

// StoreExecutor executes search plans using store.SearchQueryExecutor.
type StoreExecutor struct {
	Backend   store.SearchQueryExecutor
	Advanced  store.SearchAdvancedExecutor
	Resources store.ResourceStore
}

// NewStoreExecutor constructs an executor backed by SearchQueryExecutor.
func NewStoreExecutor(backend store.SearchQueryExecutor, resources store.ResourceStore) *StoreExecutor {
	adv, _ := backend.(store.SearchAdvancedExecutor)
	return &StoreExecutor{Backend: backend, Advanced: adv, Resources: resources}
}

// Execute runs the plan and returns matching resource IDs in deterministic order.
func (e *StoreExecutor) Execute(ctx context.Context, plan *Plan) (*ExecuteResult, error) {
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
			return &ExecuteResult{}, nil
		}
		candidateSets = append(candidateSets, ids)
	}

	for _, chainPlan := range plan.ChainPlans {
		ids, err := e.executeChainPlan(ctx, plan.ResourceType, chainPlan)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return &ExecuteResult{}, nil
		}
		candidateSets = append(candidateSets, ids)
	}

	for _, hasPlan := range plan.HasPlans {
		ids, err := e.executeHasPlan(ctx, plan.ResourceType, hasPlan)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return &ExecuteResult{}, nil
		}
		candidateSets = append(candidateSets, ids)
	}

	var scores map[string]float64
	if plan.FullText != "" {
		if e.Advanced == nil {
			return nil, fmt.Errorf("%w: full-text search", ErrUnsupportedFeature)
		}
		ft, err := e.Advanced.LookupFullText(ctx, plan.ResourceType, plan.FullText)
		if err != nil {
			return nil, err
		}
		if len(ft.IDs) == 0 {
			return &ExecuteResult{}, nil
		}
		scores = ft.Scores
		candidateSets = append(candidateSets, ft.IDs)
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

	if plan.FullText != "" && shouldRankByFullText(plan.Sort) && len(scores) > 0 {
		ids = sortByFullTextRank(ids, scores)
	} else {
		ids, err = e.sortResourceIDs(ctx, plan.ResourceType, ids, plan.Sort)
		if err != nil {
			return nil, err
		}
	}

	total := len(ids)
	start := plan.Offset
	if start > len(ids) {
		return &ExecuteResult{Total: total, Scores: scores}, nil
	}
	end := start + plan.Count
	if end > len(ids) {
		end = len(ids)
	}
	pageIDs := ids[start:end]

	included, err := e.expandIncludes(ctx, plan, pageIDs)
	if err != nil {
		return nil, err
	}

	return &ExecuteResult{
		IDs:      pageIDs,
		Total:    total,
		Scores:   scores,
		Included: included,
	}, nil
}

func shouldRankByFullText(sortFields []SortField) bool {
	if len(sortFields) == 0 {
		return true
	}
	if len(sortFields) == 1 && sortFields[0].Code == "_id" {
		return true
	}
	return false
}

func sortByFullTextRank(ids []string, scores map[string]float64) []string {
	out := append([]string(nil), ids...)
	sort.SliceStable(out, func(i, j int) bool {
		left := scores[out[i]]
		right := scores[out[j]]
		if left == right {
			return out[i] < out[j]
		}
		return left > right
	})
	return out
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
		sortFields = []SortField{{Code: "_id", FieldKey: "token._id", Direction: SortAsc}}
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
			fieldKey := field.FieldKey
			if fieldKey == "" {
				fieldKey = fieldKeyForSort(field.Code)
			}
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
		matches, err := lookupMatch(ctx, e.Backend, store.SearchMatch{
			ResourceType: resourceType,
			FieldKey:     pred.FieldKey,
			Value:        pred.Value,
			Operator:     string(pred.Operator),
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

func (e *StoreExecutor) executeChainPlan(ctx context.Context, resourceType string, chain ChainPlan) ([]string, error) {
	var targetIDs []string
	var err error
	if chain.Nested != nil {
		targetIDs, err = e.executeChainPlan(ctx, chain.TargetType, *chain.Nested)
	} else {
		targetIDs, err = e.executeParamPlan(ctx, chain.TargetType, chain.ParamPlan)
	}
	if err != nil {
		return nil, err
	}
	if len(targetIDs) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var ids []string
	for _, targetID := range targetIDs {
		for _, value := range referenceLookupValues(chain.TargetType, targetID) {
			matches, err := lookupMatch(ctx, e.Backend, store.SearchMatch{
				ResourceType: resourceType,
				FieldKey:     chain.RefFieldKey,
				Value:        value,
				Operator:     string(OpEqual),
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
	}
	sort.Strings(ids)
	return ids, nil
}

func (e *StoreExecutor) executeHasPlan(ctx context.Context, searchType string, has HasPlan) ([]string, error) {
	var sourceIDs []string
	var err error
	switch {
	case has.Nested != nil:
		sourceIDs, err = e.executeHasPlan(ctx, has.SourceType, *has.Nested)
	case has.ChainPlan != nil:
		sourceIDs, err = e.executeChainPlan(ctx, has.SourceType, *has.ChainPlan)
	default:
		sourceIDs, err = e.executeParamPlan(ctx, has.SourceType, has.ParamPlan)
	}
	if err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}

	targetIDs, err := e.referencedTargetIDs(ctx, has.SourceType, has.RefFieldKey, searchType, sourceIDs)
	if err != nil {
		return nil, err
	}
	sort.Strings(targetIDs)
	return targetIDs, nil
}

func (e *StoreExecutor) referencedTargetIDs(ctx context.Context, sourceType, refFieldKey, targetType string, sourceIDs []string) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	add := func(typ, id string) {
		if id == "" {
			return
		}
		if targetType != "" && typ != "" && typ != targetType {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if e.Advanced != nil {
		refs, err := e.Advanced.LookupReferences(ctx, sourceType, refFieldKey, sourceIDs)
		if err != nil {
			return nil, err
		}
		for _, links := range refs {
			for _, link := range links {
				typ := link.TargetType
				if typ == "" {
					typ = parseIncludedReference(link.Literal)
				}
				add(typ, link.TargetID)
			}
		}
		return ids, nil
	}

	values, err := e.Backend.FieldValues(ctx, sourceType, refFieldKey, sourceIDs)
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		link := parseReferenceLink(value)
		add(link.TargetType, link.TargetID)
	}
	return ids, nil
}

func parseReferenceLink(value string) store.ReferenceLink {
	if i := strings.Index(value, "|"); i > 0 && i < len(value)-1 {
		return store.ReferenceLink{TargetType: value[:i], TargetID: value[i+1:], Literal: value}
	}
	if i := strings.Index(value, "/"); i > 0 && i < len(value)-1 {
		return store.ReferenceLink{TargetType: value[:i], TargetID: value[i+1:], Literal: value}
	}
	return store.ReferenceLink{TargetID: value, Literal: value}
}

func referenceLookupValues(targetType, targetID string) []string {
	typed := targetType + "/" + targetID
	canonical := targetType + "|" + targetID
	if typed == targetID {
		return []string{targetID}
	}
	return []string{typed, targetID, canonical}
}

func (e *StoreExecutor) expandIncludes(ctx context.Context, plan *Plan, primaryIDs []string) ([]IncludedResource, error) {
	if len(plan.Includes) == 0 && len(plan.RevIncludes) == 0 {
		return nil, nil
	}
	if e.Advanced == nil {
		return nil, fmt.Errorf("%w: include/revinclude", ErrUnsupportedFeature)
	}
	seen := make(map[string]struct{})
	var out []IncludedResource

	for _, inc := range plan.Includes {
		refs, err := e.Advanced.LookupReferences(ctx, plan.ResourceType, inc.RefFieldKey, primaryIDs)
		if err != nil {
			return nil, err
		}
		for _, links := range refs {
			for _, link := range links {
				if inc.TargetType != "" && link.TargetType != inc.TargetType {
					continue
				}
				if link.TargetID == "" {
					continue
				}
				targetType := link.TargetType
				if targetType == "" {
					targetType = inc.TargetType
				}
				if targetType == "" && link.Literal != "" {
					if parsed := parseIncludedReference(link.Literal); parsed != "" {
						targetType = parsed
					}
				}
				if targetType == "" {
					continue
				}
				key := targetType + "/" + link.TargetID
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, IncludedResource{ResourceType: targetType, ID: link.TargetID, Mode: "include"})
			}
		}
	}

	for _, rev := range plan.RevIncludes {
		for _, primaryID := range primaryIDs {
			sourceIDs, err := e.Advanced.LookupReferencing(ctx, rev.SourceType, rev.RefFieldKey, rev.TargetType, primaryID)
			if err != nil {
				return nil, err
			}
			for _, sourceID := range sourceIDs {
				key := rev.SourceType + "/" + sourceID
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, IncludedResource{ResourceType: rev.SourceType, ID: sourceID, Mode: "include"})
			}
		}
	}
	return out, nil
}

// IncludedResource identifies one included or revincluded resource.
type IncludedResource struct {
	ResourceType string
	ID           string
	Mode         string
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

func parseIncludedReference(value string) string {
	if i := strings.Index(value, "/"); i > 0 {
		return value[:i]
	}
	if i := strings.Index(value, "|"); i > 0 {
		return value[:i]
	}
	return ""
}

func lookupMatch(ctx context.Context, backend store.SearchQueryExecutor, match store.SearchMatch) ([]string, error) {
	if backend == nil {
		return nil, fmt.Errorf("search backend is required")
	}
	matches, err := backend.LookupMatch(ctx, match)
	if err != nil {
		if errors.Is(err, store.ErrUnsupportedFeature) {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedFeature, err)
		}
		return nil, err
	}
	return matches, nil
}
