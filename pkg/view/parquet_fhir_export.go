package view

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/degoke/health-ai-stack/pkg/parquetfhir"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// MatchingResourceStats summarizes FHIR resource matching for Parquet-on-FHIR export.
type MatchingResourceStats struct {
	Scanned            int
	Filtered           int
	Written            int
	SourceResourceType string
	MaxLastUpdated     time.Time
}

// CollectMatchingResources returns full FHIR resources matching a view's filters.
//
// Deprecated: prefer WriteParquetFHIRExport for large exports. This helper materializes
// every matching resource in memory and is not suitable for lakehouse-scale datasets.
func (e *Executor) CollectMatchingResources(ctx context.Context, req ExecuteRequest) ([]map[string]any, string, error) {
	resources, resourceType, _, err := e.collectMatchingResources(ctx, req, 0, 0)
	return resources, resourceType, err
}

type matchingResourcePlan struct {
	spec     *ViewSpec
	execReq  ExecuteRequest
	limit    int
	offset   int
}

func (e *Executor) prepareMatchingResourcePlan(ctx context.Context, req ExecuteRequest, limit, offset int) (*matchingResourcePlan, error) {
	if e == nil {
		return nil, fmt.Errorf("view: executor is nil")
	}
	spec, err := e.ResolveView(req.ViewName, req.Version)
	if err != nil {
		return nil, err
	}
	if e.cfg.Authorizer != nil && len(spec.Permissions) > 0 {
		if err := e.cfg.Authorizer.AuthorizeView(ctx, AuthRequest{
			ViewName:     spec.Name,
			Version:      spec.Version,
			ResourceType: spec.ResourceType,
			Actor:        req.Actor,
			Subject:      req.Subject,
			Permissions:  spec.Permissions,
			Parameters:   req.Parameters,
		}); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
	}
	if err := spec.compile(e.cfg.Engine); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	return &matchingResourcePlan{
		spec:    spec,
		execReq: req,
		limit:   limit,
		offset:  offset,
	}, nil
}

func (e *Executor) collectMatchingResources(ctx context.Context, req ExecuteRequest, limit, offset int) ([]map[string]any, string, int, error) {
	plan, err := e.prepareMatchingResourcePlan(ctx, req, limit, offset)
	if err != nil {
		return nil, "", 0, err
	}
	resources, written, _, err := e.collectResourcesForPlan(ctx, plan)
	return resources, plan.spec.ResourceType, written, err
}

func (e *Executor) collectResourcesForPlan(ctx context.Context, plan *matchingResourcePlan) ([]map[string]any, int, MatchingResourceStats, error) {
	resources := make([]map[string]any, 0)
	written, stats, err := e.forEachMatchingResourcePlan(ctx, plan, func(raw map[string]any) error {
		resources = append(resources, raw)
		return nil
	})
	return resources, written, stats, err
}

func (e *Executor) structureDefinitionFor(resourceType string) (*validate.StructureDefinition, error) {
	if e.cfg.ProfileCatalog == nil {
		return nil, fmt.Errorf("view: ProfileCatalog is required for Parquet-on-FHIR layout")
	}
	return parquetfhir.ResolveStructureDefinition(e.cfg.ProfileCatalog, resourceType)
}

// WriteParquetFHIRExport writes Parquet-on-FHIR nested resources for one view.
func WriteParquetFHIRExport(ctx context.Context, w io.Writer, exec *Executor, req ExecuteRequest) (int, MatchingResourceStats, error) {
	if exec == nil {
		return 0, MatchingResourceStats{}, fmt.Errorf("view: executor is required")
	}
	if err := ctx.Err(); err != nil {
		return 0, MatchingResourceStats{}, err
	}

	plan, err := exec.prepareMatchingResourcePlan(ctx, req, req.Limit, req.Offset)
	if err != nil {
		return 0, MatchingResourceStats{}, err
	}
	sd, err := exec.structureDefinitionFor(plan.spec.ResourceType)
	if err != nil {
		return 0, MatchingResourceStats{}, err
	}

	stats := MatchingResourceStats{SourceResourceType: plan.spec.ResourceType}
	written, err := parquetfhir.WriteResourcesStreaming(ctx, w, sd, exec.cfg.ProfileCatalog, func(yield func(map[string]any) error) error {
		var matchErr error
		stats.Written, stats, matchErr = exec.forEachMatchingResourcePlan(ctx, plan, yield)
		return matchErr
	})
	if err != nil {
		return written, stats, err
	}
	stats.Written = written
	return written, stats, nil
}

func (e *Executor) forEachMatchingResource(
	ctx context.Context,
	req ExecuteRequest,
	limit, offset int,
	fn func(map[string]any) error,
) (int, MatchingResourceStats, error) {
	plan, err := e.prepareMatchingResourcePlan(ctx, req, limit, offset)
	if err != nil {
		return 0, MatchingResourceStats{}, err
	}
	return e.forEachMatchingResourcePlan(ctx, plan, fn)
}

func (e *Executor) forEachMatchingResourcePlan(
	ctx context.Context,
	plan *matchingResourcePlan,
	fn func(map[string]any) error,
) (int, MatchingResourceStats, error) {
	stats := MatchingResourceStats{}
	if e == nil || plan == nil || plan.spec == nil {
		return 0, stats, fmt.Errorf("view: executor is nil")
	}
	stats.SourceResourceType = plan.spec.ResourceType

	allIDs, err := e.resolveCandidateIDs(ctx, plan.spec, plan.execReq.Since)
	if err != nil {
		return 0, stats, err
	}
	stats.Scanned = len(allIDs)

	matched := 0
	written := 0
	for _, id := range allIDs {
		if err := ctx.Err(); err != nil {
			stats.Written = written
			return written, stats, err
		}
		env, err := e.cfg.Resources.Read(ctx, plan.spec.ResourceType, id)
		if err != nil {
			stats.Written = written
			return written, stats, fmt.Errorf("read %s/%s: %w", plan.spec.ResourceType, id, err)
		}
		if !plan.execReq.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(plan.execReq.Since) {
			continue
		}
		match, err := e.evalFilters(ctx, plan.spec, env)
		if err != nil {
			stats.Written = written
			return written, stats, fmt.Errorf("filter %s/%s: %w", plan.spec.ResourceType, id, err)
		}
		if !match {
			continue
		}
		stats.Filtered++
		if matched < plan.offset {
			matched++
			continue
		}
		if plan.limit > 0 && written >= plan.limit {
			break
		}
		var raw map[string]any
		if err := json.Unmarshal(env.JSON, &raw); err != nil {
			stats.Written = written
			return written, stats, fmt.Errorf("decode %s/%s: %w", plan.spec.ResourceType, id, err)
		}
		if err := fn(raw); err != nil {
			stats.Written = written
			return written, stats, err
		}
		if !env.LastUpdated.IsZero() && env.LastUpdated.After(stats.MaxLastUpdated) {
			stats.MaxLastUpdated = env.LastUpdated
		}
		matched++
		written++
	}
	stats.Written = written
	return written, stats, nil
}
