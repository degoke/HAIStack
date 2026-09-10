package search

import (
	"context"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// MatchResourceParameter reports whether a resource satisfies one search parameter value set.
// It uses the same FHIRPath extraction and normalization pipeline as the search indexer.
func MatchResourceParameter(ctx context.Context, reg Registry, engine fhirpath.Engine, resourceType string, resource *types.ResourceEnvelope, param string, want []string) (matched bool, known bool) {
	if reg == nil || engine == nil || resource == nil {
		return false, false
	}
	info, ok := reg.SearchParameter(resourceType, param)
	if !ok || strings.TrimSpace(info.Expression) == "" {
		return false, false
	}
	values, err := engine.Eval(ctx, info.Expression, resource)
	if err != nil {
		return false, true
	}
	normalized := normalizeValues(info.Code, info.Type, values)
	return valuesMatchFilter(want, normalized), true
}

func valuesMatchFilter(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	if len(have) == 0 {
		return false
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[strings.ToLower(strings.TrimSpace(w))] = struct{}{}
	}
	for _, value := range have {
		lower := strings.ToLower(strings.TrimSpace(value))
		if _, ok := wantSet[lower]; ok {
			return true
		}
		if idx := strings.LastIndex(lower, "|"); idx >= 0 {
			if _, ok := wantSet[lower[idx+1:]]; ok {
				return true
			}
		}
	}
	return false
}
