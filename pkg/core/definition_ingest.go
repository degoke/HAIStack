package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const fhirAPIDefinitionModule = "fhir-api"

// DefinitionIngestor installs FHIR definition resources into the registry catalog.
type DefinitionIngestor interface {
	InstallDefinition(ctx context.Context, jsonData []byte, provenance registry.InstallProvenance) error
	DeleteDefinition(ctx context.Context, canonicalURL, version string) error
}

func (s *ResourceService) ingestDefinitionResource(ctx context.Context, env *types.ResourceEnvelope) error {
	if s.definitionIngestor == nil || env == nil {
		return nil
	}
	switch env.ResourceType {
	case "StructureDefinition", "SearchParameter", "CodeSystem", "ValueSet", "ConceptMap":
	default:
		return nil
	}
	var meta struct {
		URL     string `json:"url"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(env.JSON, &meta); err != nil {
		return err
	}
	if strings.TrimSpace(meta.URL) == "" {
		return fmt.Errorf("definition url is required")
	}
	if strings.TrimSpace(meta.Version) == "" {
		meta.Version = "1"
	}
	provenance := registry.InstallProvenance{
		PackageName:    fhirAPIDefinitionModule,
		PackageVersion: meta.Version,
		ModuleName:     fhirAPIDefinitionModule,
		SourceModule:   fhirAPIDefinitionModule,
	}
	if err := s.definitionIngestor.InstallDefinition(ctx, env.JSON, provenance); err != nil {
		return err
	}
	if s.conformanceRefresh != nil {
		return s.conformanceRefresh(ctx)
	}
	return nil
}

func (s *ResourceService) removeDefinitionResource(ctx context.Context, env *types.ResourceEnvelope) error {
	if s.definitionIngestor == nil || env == nil {
		return nil
	}
	switch env.ResourceType {
	case "StructureDefinition", "SearchParameter", "CodeSystem", "ValueSet", "ConceptMap":
	default:
		return nil
	}
	var meta struct {
		URL     string `json:"url"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(env.JSON, &meta); err != nil {
		return err
	}
	if strings.TrimSpace(meta.URL) == "" {
		return nil
	}
	if strings.TrimSpace(meta.Version) == "" {
		meta.Version = "1"
	}
	if err := s.definitionIngestor.DeleteDefinition(ctx, meta.URL, meta.Version); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return err
	}
	if s.conformanceRefresh != nil {
		return s.conformanceRefresh(ctx)
	}
	return nil
}
