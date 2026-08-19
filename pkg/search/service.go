package search

import (
	"context"
	"fmt"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ServiceConfig configures the high-level search service.
type ServiceConfig struct {
	Registry  Registry
	Executor  Executor
	Planner   Planner
	Resources store.ResourceStore
	BaseURL   string
}

// Service is the main entrypoint for FHIR search execution.
type Service struct {
	registry  Registry
	executor  Executor
	planner   Planner
	resources store.ResourceStore
	baseURL   string
}

// NewService constructs a search service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("search: registry is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("search: executor is required")
	}
	if cfg.Resources == nil {
		return nil, fmt.Errorf("search: resource store is required")
	}
	planner := cfg.Planner
	if planner == nil {
		planner = NewPlanner()
	}
	return &Service{
		registry:  cfg.Registry,
		executor:  cfg.Executor,
		planner:   planner,
		resources: cfg.Resources,
		baseURL:   cfg.BaseURL,
	}, nil
}

// Search executes a FHIR search for one resource type and returns bundle-ready results.
func (s *Service) Search(ctx context.Context, resourceType string, params url.Values) (*Result, error) {
	return s.SearchRequest(ctx, Request{ResourceType: resourceType, Params: params})
}

// SearchRequest executes a FHIR search from a structured request.
func (s *Service) SearchRequest(ctx context.Context, req Request) (*Result, error) {
	plan, err := s.planner.PlanSearch(s.registry, req.ResourceType, req.Params)
	if err != nil {
		return nil, err
	}

	execResult, err := s.executor.Execute(ctx, plan)
	if err != nil {
		return nil, err
	}

	if plan.Summary == SummaryCount {
		total := execResult.Total
		return &Result{
			ResourceType: req.ResourceType,
			Total:        &total,
			Count:        0,
			Summary:      plan.Summary,
		}, nil
	}

	resources := make([]*types.ResourceEnvelope, 0, len(execResult.IDs))
	for _, id := range execResult.IDs {
		res, err := s.resources.Read(ctx, req.ResourceType, id)
		if err != nil {
			return nil, fmt.Errorf("search: read %s/%s: %w", req.ResourceType, id, err)
		}
		projected, err := applyProjection(res, plan.Summary, plan.Elements)
		if err != nil {
			return nil, err
		}
		resources = append(resources, projected)
	}

	var included []IncludedEntry
	for _, ref := range execResult.Included {
		res, err := s.resources.Read(ctx, ref.ResourceType, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("search: read included %s/%s: %w", ref.ResourceType, ref.ID, err)
		}
		projected, err := applyProjection(res, plan.Summary, plan.Elements)
		if err != nil {
			return nil, err
		}
		mode := ref.Mode
		if mode == "" {
			mode = "include"
		}
		included = append(included, IncludedEntry{
			ResourceType: ref.ResourceType,
			ID:           ref.ID,
			Resource:     projected,
			Mode:         mode,
		})
	}

	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = req.ResourceType
	}
	totalCopy := execResult.Total
	return &Result{
		ResourceType: req.ResourceType,
		Resources:    resources,
		Included:     included,
		Total:        &totalCopy,
		Offset:       plan.Offset,
		Count:        len(resources),
		Summary:      plan.Summary,
		Elements:     append([]string(nil), plan.Elements...),
		Links:        BuildPagingLinks(baseURL, req.Params, plan.Offset, len(resources), plan.Count, &totalCopy),
	}, nil
}

// SearchBundle executes search and returns a bundle-ready payload.
func (s *Service) SearchBundle(ctx context.Context, resourceType string, params url.Values) (*SearchBundle, error) {
	result, err := s.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	return AssembleBundle(result), nil
}

// SearchParametersFor returns compiled search parameter metadata for a resource type.
func (s *Service) SearchParametersFor(resourceType string) []ParameterInfo {
	if s == nil || s.registry == nil {
		return nil
	}
	return s.registry.SearchParametersFor(resourceType)
}

// EnabledResourceTypes returns resource types enabled in the search registry.
func (s *Service) EnabledResourceTypes() []string {
	if s == nil || s.registry == nil {
		return nil
	}
	return s.registry.EnabledResourceTypes()
}
