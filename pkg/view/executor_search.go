package view

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/search"
)

// SearchMode controls whether view execution uses the search index.
type SearchMode string

const (
	// SearchModeAuto uses search when configured and search params are available.
	SearchModeAuto SearchMode = "auto"
	// SearchModeIndex requires search-backed candidate resolution.
	SearchModeIndex SearchMode = "index"
	// SearchModeScan forces ListIDs scanning.
	SearchModeScan SearchMode = "scan"
)

func (e *Executor) resolveCandidateIDs(ctx context.Context, spec *ViewSpec, since time.Time) ([]string, error) {
	mode := spec.SearchMode
	if mode == "" {
		mode = SearchModeAuto
	}
	useSearch := e.cfg.Search != nil && e.cfg.SearchRegistry != nil
	if mode == SearchModeScan {
		useSearch = false
	}
	if mode == SearchModeIndex && !useSearch {
		return nil, fmt.Errorf("%w: search index is required for metadata.searchMode=index", ErrUnsupportedFeature)
	}
	if !useSearch {
		return e.listAllResourceIDs(ctx, spec.ResourceType)
	}

	params := cloneValues(spec.SearchParams)
	if params == nil {
		params = url.Values{}
	}
	if !since.IsZero() {
		params.Set("_lastUpdated", "gt"+since.UTC().Format(time.RFC3339))
	}
	planner := e.cfg.SearchPlanner
	if planner == nil {
		planner = search.NewPlanner()
	}
	plan, err := planner.PlanSearch(e.cfg.SearchRegistry, spec.ResourceType, params)
	if err != nil {
		if mode == SearchModeAuto {
			return e.listAllResourceIDs(ctx, spec.ResourceType)
		}
		return nil, fmt.Errorf("plan search for view: %w", err)
	}
	plan.Offset = 0
	plan.Count = 0
	result, err := e.cfg.Search.Execute(ctx, plan)
	if err != nil {
		if mode == SearchModeAuto {
			return e.listAllResourceIDs(ctx, spec.ResourceType)
		}
		return nil, fmt.Errorf("execute search for view: %w", err)
	}
	return result.IDs, nil
}

func (e *Executor) listAllResourceIDs(ctx context.Context, resourceType string) ([]string, error) {
	var allIDs []string
	pageSize := 100
	for {
		ids, err := e.cfg.Resources.ListIDs(ctx, resourceType, pageSize, len(allIDs))
		if err != nil {
			return nil, fmt.Errorf("list %s IDs: %w", resourceType, err)
		}
		if len(ids) == 0 {
			break
		}
		allIDs = append(allIDs, ids...)
	}
	return allIDs, nil
}

func cloneValues(v url.Values) url.Values {
	if len(v) == 0 {
		return nil
	}
	out := make(url.Values, len(v))
	for key, vals := range v {
		copied := append([]string(nil), vals...)
		out[key] = copied
	}
	return out
}

func parseSearchMetadata(metadata map[string]string) (url.Values, SearchMode) {
	if metadata == nil {
		return nil, SearchModeAuto
	}
	mode := SearchMode(strings.ToLower(strings.TrimSpace(metadata["searchMode"])))
	switch mode {
	case SearchModeIndex, SearchModeScan:
	default:
		mode = SearchModeAuto
	}
	raw := strings.TrimSpace(metadata["searchParams"])
	if raw == "" {
		return nil, mode
	}
	parsed, err := url.ParseQuery(raw)
	if err != nil {
		return nil, mode
	}
	return parsed, mode
}
