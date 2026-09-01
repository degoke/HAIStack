package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const fhirAPIDefinitionModule = "fhir-api"

var definitionResourceTypes = map[string]struct{}{
	"StructureDefinition": {},
	"SearchParameter":     {},
	"CodeSystem":          {},
	"ValueSet":            {},
	"ConceptMap":          {},
}

// DefinitionIngestor installs FHIR definition resources into the registry catalog.
type DefinitionIngestor interface {
	InstallDefinition(ctx context.Context, jsonData []byte, provenance registry.InstallProvenance) error
	DeleteDefinition(ctx context.Context, canonicalURL, version string) error
}

func (s *ResourceService) ingestDefinitionResource(ctx context.Context, env *types.ResourceEnvelope) error {
	if err := s.ingestDefinitionResourceWithoutRefresh(ctx, env); err != nil {
		return err
	}
	return s.refreshConformance(ctx)
}

func (s *ResourceService) removeDefinitionResource(ctx context.Context, env *types.ResourceEnvelope) error {
	if err := s.removeDefinitionResourceWithoutRefresh(ctx, env); err != nil {
		return err
	}
	return s.refreshConformance(ctx)
}

func (s *ResourceService) syncDefinitionCatalog(ctx context.Context, written, deleted []*types.ResourceEnvelope) error {
	for _, env := range written {
		if err := s.ingestDefinitionResourceWithoutRefresh(ctx, env); err != nil {
			return err
		}
	}
	for _, env := range deleted {
		if err := s.removeDefinitionResourceWithoutRefresh(ctx, env); err != nil {
			return err
		}
	}
	return s.refreshConformance(ctx)
}

func (s *ResourceService) refreshConformance(ctx context.Context) error {
	if s.conformanceRefresh != nil {
		return s.conformanceRefresh(ctx)
	}
	return nil
}

func (s *ResourceService) ingestDefinitionResourceWithoutRefresh(ctx context.Context, env *types.ResourceEnvelope) error {
	if s.definitionIngestor == nil || env == nil {
		return nil
	}
	if !isDefinitionResourceType(env.ResourceType) {
		return nil
	}
	meta, err := definitionMetaFromEnvelope(env)
	if err != nil {
		return err
	}
	provenance := registry.InstallProvenance{
		PackageName:    fhirAPIDefinitionModule,
		PackageVersion: meta.Version,
		ModuleName:     fhirAPIDefinitionModule,
		SourceModule:   fhirAPIDefinitionModule,
	}
	return s.definitionIngestor.InstallDefinition(ctx, env.JSON, provenance)
}

func (s *ResourceService) removeDefinitionResourceWithoutRefresh(ctx context.Context, env *types.ResourceEnvelope) error {
	if s.definitionIngestor == nil || env == nil {
		return nil
	}
	if !isDefinitionResourceType(env.ResourceType) {
		return nil
	}
	meta, err := definitionMetaFromEnvelope(env)
	if err != nil {
		return err
	}
	if meta.URL == "" {
		return nil
	}
	if err := s.definitionIngestor.DeleteDefinition(ctx, meta.URL, meta.Version); err != nil {
		if isDefinitionNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func isDefinitionResourceType(resourceType string) bool {
	_, ok := definitionResourceTypes[resourceType]
	return ok
}

func definitionMetaFromEnvelope(env *types.ResourceEnvelope) (struct {
	URL     string
	Version string
}, error) {
	var meta struct {
		URL     string `json:"url"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(env.JSON, &meta); err != nil {
		return struct {
			URL     string
			Version string
		}{}, err
	}
	if strings.TrimSpace(meta.URL) == "" {
		return struct {
			URL     string
			Version string
		}{}, fmt.Errorf("definition url is required")
	}
	if strings.TrimSpace(meta.Version) == "" {
		meta.Version = registry.DefaultFHIRVersion
	}
	return struct {
		URL     string
		Version string
	}{URL: meta.URL, Version: meta.Version}, nil
}

func isDefinitionNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, registry.ErrDefinitionNotFound) ||
		strings.Contains(strings.ToLower(err.Error()), "definition not found")
}
