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
		switch param.Type {
		case "composite":
			values, err := i.indexComposite(ctx, resource, param)
			if err != nil {
				return nil, err
			}
			if len(values) > 0 {
				key := compositeFieldKey(param.Code)
				fieldValues[key] = append(fieldValues[key], values...)
			}
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

	if doc := buildFullTextDocument(fieldValues); doc != "" {
		fieldValues["text.document"] = []string{doc}
	}

	return flattenEntries(resource.ResourceType, resource.ID, fieldValues), nil
}

func (i *RegistryIndexer) indexComposite(ctx context.Context, resource *types.ResourceEnvelope, param ParameterInfo) ([]string, error) {
	if len(param.Component) == 0 {
		return nil, nil
	}
	var components []string
	for _, comp := range param.Component {
		expr := comp.Expression
		if expr == "" && comp.Code != "" {
			if info, ok := i.registry.SearchParameter(resource.ResourceType, comp.Code); ok {
				expr = info.Expression
			}
		}
		if expr == "" {
			return nil, fmt.Errorf("search: composite component %q missing expression", comp.Code)
		}
		values, err := i.engine.Eval(ctx, expr, resource)
		if err != nil {
			if isSkippableEvalError(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("search: evaluate composite component %s: %w", comp.Code, err)
		}
		compType := comp.Type
		if compType == "" {
			compType = "string"
		}
		normalized := normalizeValues(comp.Code, compType, values)
		if len(normalized) == 0 {
			return nil, nil
		}
		components = append(components, normalized[0])
	}
	if len(components) != len(param.Component) {
		return nil, nil
	}
	return []string{compositeIndexValue(components)}, nil
}

func buildFullTextDocument(fieldValues map[string][]string) string {
	var parts []string
	for key, values := range fieldValues {
		if !strings.HasPrefix(key, "string.") {
			continue
		}
		parts = append(parts, values...)
	}
	if len(parts) == 0 {
		return ""
	}
	seen := make(map[string]struct{})
	var unique []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}
	return strings.Join(unique, " ")
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
