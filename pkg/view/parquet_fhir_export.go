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
	if e == nil {
		return nil, "", fmt.Errorf("view: executor is nil")
	}
	spec, err := e.ResolveView(req.ViewName, req.Version)
	if err != nil {
		return nil, "", err
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
			return nil, "", fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
	}
	if err := spec.compile(e.cfg.Engine); err != nil {
		return nil, "", err
	}

	allIDs, err := e.resolveCandidateIDs(ctx, spec, req.Since)
	if err != nil {
		return nil, "", err
	}

	resources := make([]map[string]any, 0, len(allIDs))
	for _, id := range allIDs {
		env, err := e.cfg.Resources.Read(ctx, spec.ResourceType, id)
		if err != nil {
			return nil, "", fmt.Errorf("read %s/%s: %w", spec.ResourceType, id, err)
		}
		if !req.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(req.Since) {
			continue
		}
		match, err := e.evalFilters(ctx, spec, env)
		if err != nil {
			return nil, "", fmt.Errorf("filter %s/%s: %w", spec.ResourceType, id, err)
		}
		if !match {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(env.JSON, &raw); err != nil {
			return nil, "", fmt.Errorf("decode %s/%s: %w", spec.ResourceType, id, err)
		}
		resources = append(resources, raw)
	}
	return resources, spec.ResourceType, nil
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
	resources, resourceType, err := exec.CollectMatchingResources(ctx, req)
	if err != nil {
		return 0, err
	}
	sd, err := exec.structureDefinitionFor(resourceType)
	if err != nil {
		return 0, err
	}
	if err := parquetfhir.WriteResources(w, sd, resources); err != nil {
		return 0, err
	}
	return len(resources), nil
}
