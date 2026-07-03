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
	Resources store.ResourceStore
	BaseURL   string
}

// Service is the main entrypoint for FHIR search execution.
type Service struct {
	registry  Registry
	executor  Executor
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
	return &Service{
		registry:  cfg.Registry,
		executor:  cfg.Executor,
		resources: cfg.Resources,
		baseURL:   cfg.BaseURL,
	}, nil
}

// Search executes a FHIR search for one resource type and returns bundle-ready results.
func (s *Service) Search(ctx context.Context, resourceType string, params url.Values) (*Result, error) {
	parsed, err := ParseQuery(resourceType, params)
	if err != nil {
		return nil, err
	}
	resolved, err := ResolveQuery(s.registry, parsed)
	if err != nil {
		return nil, err
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		return nil, err
	}

	allIDs, err := s.executor.Execute(ctx, planWithoutPaging(plan))
	if err != nil {
		return nil, err
	}
	total := len(allIDs)

	pageIDs, err := s.executor.Execute(ctx, plan)
	if err != nil {
		return nil, err
	}

	resources := make([]*types.ResourceEnvelope, 0, len(pageIDs))
	for _, id := range pageIDs {
		res, err := s.resources.Read(ctx, resourceType, id)
		if err != nil {
			return nil, fmt.Errorf("search: read %s/%s: %w", resourceType, id, err)
		}
		resources = append(resources, res)
	}

	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = resourceType
	}
	totalCopy := total
	return &Result{
		ResourceType: resourceType,
		Resources:    resources,
		Total:        &totalCopy,
		Offset:       plan.Offset,
		Count:        len(resources),
		Links:        BuildPagingLinks(baseURL, plan.Offset, len(resources), plan.Count, &totalCopy),
	}, nil
}

func planWithoutPaging(plan *Plan) *Plan {
	if plan == nil {
		return nil
	}
	copyPlan := *plan
	copyPlan.Offset = 0
	copyPlan.Count = 1_000_000
	return &copyPlan
}

// SearchBundle executes search and returns a bundle-ready payload.
func (s *Service) SearchBundle(ctx context.Context, resourceType string, params url.Values) (*SearchBundle, error) {
	result, err := s.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	return AssembleBundle(result), nil
}
