package view

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/parquetfhir"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// CollectMatchingResources returns full FHIR resources matching a view's filters.
func (e *Executor) CollectMatchingResources(ctx context.Context, req ExecuteRequest) ([]map[string]any, string, error) {
	resources, resourceType, _, err := e.collectMatchingResources(ctx, req, 0, 0)
	return resources, resourceType, err
}

func (e *Executor) collectMatchingResources(ctx context.Context, req ExecuteRequest, limit, offset int) ([]map[string]any, string, int, error) {
	if e == nil {
		return nil, "", 0, fmt.Errorf("view: executor is nil")
	}
	spec, err := e.ResolveView(req.ViewName, req.Version)
	if err != nil {
		return nil, "", 0, err
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
			return nil, "", 0, fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
	}
	if err := spec.compile(e.cfg.Engine); err != nil {
		return nil, "", 0, err
	}

	allIDs, err := e.resolveCandidateIDs(ctx, spec, req.Since)
	if err != nil {
		return nil, "", 0, err
	}

	resources := make([]map[string]any, 0)
	matched := 0
	for _, id := range allIDs {
		if err := ctx.Err(); err != nil {
			return nil, "", matched, err
		}
		env, err := e.cfg.Resources.Read(ctx, spec.ResourceType, id)
		if err != nil {
			return nil, "", matched, fmt.Errorf("read %s/%s: %w", spec.ResourceType, id, err)
		}
		if !req.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(req.Since) {
			continue
		}
		match, err := e.evalFilters(ctx, spec, env)
		if err != nil {
			return nil, "", matched, fmt.Errorf("filter %s/%s: %w", spec.ResourceType, id, err)
		}
		if !match {
			continue
		}
		if matched < offset {
			matched++
			continue
		}
		if limit > 0 && len(resources) >= limit {
			break
		}
		var raw map[string]any
		if err := json.Unmarshal(env.JSON, &raw); err != nil {
			return nil, "", matched, fmt.Errorf("decode %s/%s: %w", spec.ResourceType, id, err)
		}
		resources = append(resources, raw)
		matched++
	}
	return resources, spec.ResourceType, matched, nil
}

func (e *Executor) structureDefinitionFor(resourceType string) (*validate.StructureDefinition, error) {
	if e.cfg.ProfileCatalog == nil {
		return nil, fmt.Errorf("view: ProfileCatalog is required for Parquet-on-FHIR layout")
	}
	return parquetfhir.ResolveStructureDefinition(e.cfg.ProfileCatalog, resourceType)
}

// WriteParquetFHIRExport writes Parquet-on-FHIR nested resources for one view.
func WriteParquetFHIRExport(ctx context.Context, w io.Writer, exec *Executor, req ExecuteRequest) (int, error) {
	if exec == nil {
		return 0, fmt.Errorf("view: executor is required")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	spec, err := exec.ResolveView(req.ViewName, req.Version)
	if err != nil {
		return 0, err
	}
	sd, err := exec.structureDefinitionFor(spec.ResourceType)
	if err != nil {
		return 0, err
	}

	limit := req.Limit
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	return parquetfhir.WriteResourcesStreaming(ctx, w, sd, func(yield func(map[string]any) error) error {
		resources, _, _, err := exec.collectMatchingResources(ctx, req, limit, offset)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if err := yield(resource); err != nil {
				return err
			}
		}
		return nil
	})
}
