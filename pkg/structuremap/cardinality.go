package structuremap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// CardinalityResolver reports whether a FHIR element path allows multiple values.
type CardinalityResolver interface {
	IsRepeating(ctx context.Context, fhirPath string) (bool, bool)
}

// StoreCardinalityResolver resolves cardinality from StructureDefinition resources.
type StoreCardinalityResolver struct {
	Store store.DefinitionStore
	mu    sync.Mutex
	cache map[string]map[string]string // resourceType -> element path -> max
}

func (r *StoreCardinalityResolver) IsRepeating(ctx context.Context, fhirPath string) (bool, bool) {
	if r == nil || r.Store == nil || fhirPath == "" {
		return false, false
	}
	resourceType, elementPath, ok := splitFHIRElementPath(fhirPath)
	if !ok {
		return false, false
	}
	max, err := r.elementMax(ctx, resourceType, elementPath)
	if err != nil || max == "" {
		return false, false
	}
	return isRepeatingMax(max), true
}

func (r *StoreCardinalityResolver) elementMax(ctx context.Context, resourceType, elementPath string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = map[string]map[string]string{}
	}
	if cached, ok := r.cache[resourceType][elementPath]; ok {
		return cached, nil
	}
	index, err := r.loadIndex(ctx, resourceType)
	if err != nil {
		return "", err
	}
	if r.cache[resourceType] == nil {
		r.cache[resourceType] = map[string]string{}
	}
	for path, max := range index {
		r.cache[resourceType][path] = max
	}
	return index[elementPath], nil
}

func (r *StoreCardinalityResolver) loadIndex(ctx context.Context, resourceType string) (map[string]string, error) {
	canonical := "http://hl7.org/fhir/StructureDefinition/" + resourceType
	record, err := r.Store.Get(ctx, canonical, "")
	if err != nil || record == nil || len(record.JSONData) == 0 {
		return nil, fmt.Errorf("StructureDefinition %s not found", canonical)
	}
	var sd map[string]any
	if err := json.Unmarshal(record.JSONData, &sd); err != nil {
		return nil, fmt.Errorf("parse StructureDefinition %s: %w", canonical, err)
	}
	elements := structureDefinitionElements(sd)
	index := map[string]string{}
	for _, raw := range elements {
		element, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := element["path"].(string)
		if path == "" || !strings.HasPrefix(path, resourceType+".") {
			continue
		}
		max, _ := element["max"].(string)
		index[path] = max
	}
	return index, nil
}

func structureDefinitionElements(sd map[string]any) []any {
	for _, key := range []string{"snapshot", "differential"} {
		section, _ := sd[key].(map[string]any)
		if section == nil {
			continue
		}
		elements, _ := section["element"].([]any)
		if len(elements) > 0 {
			return elements
		}
	}
	return nil
}

func splitFHIRElementPath(fhirPath string) (resourceType, elementPath string, ok bool) {
	fhirPath = strings.TrimSpace(fhirPath)
	if fhirPath == "" {
		return "", "", false
	}
	parts := strings.Split(fhirPath, ".")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], fhirPath, true
}

func isRepeatingMax(max string) bool {
	switch max {
	case "", "*":
		return true
	default:
		n, err := parsePositiveInt(max)
		return err == nil && n > 1
	}
}

func parsePositiveInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty integer")
	}
	var n int
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid integer %q", value)
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

func fhirElementPath(root map[string]any, elements []string) string {
	resourceType, _ := root["resourceType"].(string)
	if resourceType == "" || len(elements) == 0 {
		return ""
	}
	return resourceType + "." + strings.Join(elements, ".")
}

func isRepeatingAtPath(ctx context.Context, root map[string]any, elements []string, resolver CardinalityResolver) bool {
	if resolver != nil {
		path := fhirElementPath(root, elements)
		if path != "" {
			if repeating, ok := resolver.IsRepeating(ctx, path); ok {
				return repeating
			}
		}
	}
	if len(elements) == 0 {
		return false
	}
	return isRepeatingFieldHeuristic(root, elements[len(elements)-1])
}
