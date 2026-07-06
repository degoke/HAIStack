package search

import (
	"fmt"
	"net/url"
)

// Request is a FHIR search request for one resource type.
type Request struct {
	ResourceType string
	Params       url.Values
}

// Planner converts raw FHIR search parameters into backend-neutral plans.
type Planner interface {
	Parse(resourceType string, params url.Values) (*Query, error)
	Resolve(reg Registry, q *Query) (*Query, error)
	Build(q *Query) (*Plan, error)
	PlanSearch(reg Registry, resourceType string, params url.Values) (*Plan, error)
}

// DefaultPlanner implements Planner using package-level parse/plan functions.
type DefaultPlanner struct{}

// NewPlanner returns the default search planner.
func NewPlanner() Planner {
	return DefaultPlanner{}
}

func (DefaultPlanner) Parse(resourceType string, params url.Values) (*Query, error) {
	return ParseQuery(resourceType, params)
}

func (DefaultPlanner) Resolve(reg Registry, q *Query) (*Query, error) {
	return ResolveQuery(reg, q)
}

func (DefaultPlanner) Build(q *Query) (*Plan, error) {
	return BuildPlan(q)
}

func (p DefaultPlanner) PlanSearch(reg Registry, resourceType string, params url.Values) (*Plan, error) {
	parsed, err := p.Parse(resourceType, params)
	if err != nil {
		return nil, err
	}
	resolved, err := p.Resolve(reg, parsed)
	if err != nil {
		return nil, err
	}
	return p.Build(resolved)
}

// PlanSearch is a convenience helper that parses, resolves, and builds a plan.
func PlanSearch(reg Registry, resourceType string, params url.Values) (*Plan, error) {
	return NewPlanner().PlanSearch(reg, resourceType, params)
}

// PlanSearchRequest plans a structured search request.
func PlanSearchRequest(reg Registry, req Request) (*Plan, error) {
	if req.ResourceType == "" {
		return nil, fmt.Errorf("%w: resource type required", ErrInvalidQuery)
	}
	params := req.Params
	if params == nil {
		params = url.Values{}
	}
	return PlanSearch(reg, req.ResourceType, params)
}
