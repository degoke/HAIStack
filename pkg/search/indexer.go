package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// RegistryIndexerConfig configures a registry-driven search indexer.
type RegistryIndexerConfig struct {
	Registry Registry
	Engine   fhirpath.Engine
}

// RegistryIndexer extracts typed search index rows from registry SearchParameters.
type RegistryIndexer struct {
	registry Registry
	engine   fhirpath.Engine
}

// NewRegistryIndexer constructs a registry-driven Indexer for pkg/core.
func NewRegistryIndexer(cfg RegistryIndexerConfig) (*RegistryIndexer, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("search: registry is required")
	}
	if cfg.Engine == nil {
		return nil, fmt.Errorf("search: fhirpath engine is required")
	}
	return &RegistryIndexer{
		registry: cfg.Registry,
		engine:   cfg.Engine,
	}, nil
}

// Build implements search.Indexer.
func (i *RegistryIndexer) Build(ctx context.Context, resource *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	if resource == nil {
		return nil, fmt.Errorf("search: resource envelope is nil")
	}
	if !i.registry.IsResourceEnabled(resource.ResourceType) {
		return nil, nil
	}

	fieldValues := make(map[string][]string)

	if resource.ID != "" {
		fieldValues["token._id"] = append(fieldValues["token._id"], resource.ID)
	}
	if ts := envelopeLastUpdatedValue(resource.LastUpdated); ts != "" {
		fieldValues["date._lastUpdated"] = append(fieldValues["date._lastUpdated"], ts)
	}

	params := i.registry.SearchParametersFor(resource.ResourceType)
	for _, param := range params {
		if param.Code == "_id" || param.Code == "_lastUpdated" {
			continue
		}
		key := fieldKeyForParam(param.Code, param.Type)
		if key == "" {
			continue
		}
		if param.Expression == "" {
			continue
		}
		values, err := i.engine.Eval(ctx, param.Expression, resource)
		if err != nil {
			if isSkippableEvalError(err) {
				continue
			}
			return nil, fmt.Errorf("search: evaluate %s: %w", param.Code, err)
		}
		normalized := normalizeValues(param.Code, param.Type, values)
		if len(normalized) == 0 {
			continue
		}
		fieldValues[key] = append(fieldValues[key], normalized...)
	}

	return flattenEntries(resource.ResourceType, resource.ID, fieldValues), nil
}

func flattenEntries(resourceType, id string, fieldValues map[string][]string) []store.SearchIndexEntry {
	var entries []store.SearchIndexEntry
	for key, values := range fieldValues {
		for _, value := range values {
			if value == "" {
				continue
			}
			entries = append(entries, store.SearchIndexEntry{
				ResourceType: resourceType,
				ID:           id,
				Fields:       map[string]string{key: value},
			})
		}
	}
	return entries
}

func isSkippableEvalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fhirpath.ErrNotSupported) {
		return true
	}
	return strings.Contains(err.Error(), "resolve()")
}
