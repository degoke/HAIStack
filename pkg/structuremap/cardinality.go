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

// ResourceCardinalityResolver resolves cardinality using resource instance metadata.
type ResourceCardinalityResolver interface {
	CardinalityResolver
	IsRepeatingFor(ctx context.Context, root map[string]any, fhirPath string) (bool, bool)
}

// StoreCardinalityResolver resolves cardinality from StructureDefinition resources.
// Use cardinalityForMap during map execution for profile-aware, per-run indexes.
type StoreCardinalityResolver struct {
	Store store.DefinitionStore
}

func (r *StoreCardinalityResolver) IsRepeating(ctx context.Context, fhirPath string) (bool, bool) {
	if r == nil || r.Store == nil {
		return false, false
	}
	resolver := newMapCardinalityResolver(r.Store, Map{})
	return resolver.IsRepeating(ctx, fhirPath)
}

type mapCardinalityResolver struct {
	store    store.DefinitionStore
	profiles map[string]string
	mu       sync.Mutex
	indexes  map[string]map[string]string
}

func newMapCardinalityResolver(store store.DefinitionStore, m Map) *mapCardinalityResolver {
	return &mapCardinalityResolver{
		store:    store,
		profiles: targetProfilesFromMap(context.Background(), store, m),
		indexes:  map[string]map[string]string{},
	}
}

func (e Engine) cardinalityForMap(m Map) CardinalityResolver {
	if base, ok := e.Cardinality.(*StoreCardinalityResolver); ok && base != nil && base.Store != nil {
		return newMapCardinalityResolver(base.Store, m)
	}
	return e.Cardinality
}

func (r *mapCardinalityResolver) IsRepeating(ctx context.Context, fhirPath string) (bool, bool) {
	return r.lookup(ctx, "", fhirPath)
}

func (r *mapCardinalityResolver) IsRepeatingFor(ctx context.Context, root map[string]any, fhirPath string) (bool, bool) {
	resourceType, _, ok := splitFHIRElementPath(fhirPath)
	if !ok {
		return false, false
	}
	profileURL := profileURLFromResource(root, r.profiles[resourceType])
	return r.lookup(ctx, profileURL, fhirPath)
}

func (r *mapCardinalityResolver) lookup(ctx context.Context, profileURL, fhirPath string) (bool, bool) {
	resourceType, elementPath, ok := splitFHIRElementPath(fhirPath)
	if !ok {
		return false, false
	}
	if profileURL == "" {
		profileURL = r.profiles[resourceType]
	}
	if profileURL != "" {
		if max, found, err := r.elementMax(ctx, profileURL, resourceType, elementPath); err == nil && found {
			return isRepeatingMax(max), true
		}
	}
	baseCanonical := baseStructureDefinitionURL(resourceType)
	if max, found, err := r.elementMax(ctx, baseCanonical, resourceType, elementPath); err == nil && found {
		return isRepeatingMax(max), true
	}
	return false, false
}

func (r *mapCardinalityResolver) elementMax(ctx context.Context, canonicalURL, resourceType, elementPath string) (string, bool, error) {
	index, err := r.loadIndex(ctx, canonicalURL, resourceType)
	if err != nil {
		return "", false, err
	}
	max, ok := index[elementPath]
	if !ok {
		return "", false, nil
	}
	if max == "" {
		max = "1"
	}
	return max, true, nil
}

func (r *mapCardinalityResolver) loadIndex(ctx context.Context, canonicalURL, resourceType string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.indexes[canonicalURL]; ok {
		return cached, nil
	}
	record, err := r.store.Get(ctx, canonicalURL, "")
	if err != nil || record == nil || len(record.JSONData) == 0 {
		return nil, fmt.Errorf("StructureDefinition %s not found", canonicalURL)
	}
	var sd map[string]any
	if err := json.Unmarshal(record.JSONData, &sd); err != nil {
		return nil, fmt.Errorf("parse StructureDefinition %s: %w", canonicalURL, err)
	}
	elements := structureDefinitionElements(sd)
	index := map[string]string{}
	for _, raw := range elements {
		element, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := element["path"].(string)
		if path == "" {
			continue
		}
		if resourceType != "" && !strings.HasPrefix(path, resourceType+".") && path != resourceType {
			continue
		}
		max, _ := element["max"].(string)
		index[path] = max
	}
	r.indexes[canonicalURL] = index
	return index, nil
}

func targetProfilesFromMap(ctx context.Context, store store.DefinitionStore, m Map) map[string]string {
	profiles := map[string]string{}
	for _, structure := range m.Structure {
		if !strings.EqualFold(strings.TrimSpace(structure.Mode), "target") {
			continue
		}
		url := strings.TrimSpace(structure.URL)
		if url == "" {
			continue
		}
		resourceType := resourceTypeForStructureURL(ctx, store, url)
		if resourceType != "" {
			profiles[resourceType] = url
		}
	}
	return profiles
}

func resourceTypeForStructureURL(ctx context.Context, store store.DefinitionStore, canonicalURL string) string {
	const prefix = "http://hl7.org/fhir/StructureDefinition/"
	if strings.HasPrefix(canonicalURL, prefix) {
		return strings.TrimPrefix(canonicalURL, prefix)
	}
	if store != nil {
		record, err := store.Get(ctx, canonicalURL, "")
		if err == nil && record != nil && len(record.JSONData) > 0 {
			var sd map[string]any
			if err := json.Unmarshal(record.JSONData, &sd); err == nil {
				if resourceType, _ := sd["type"].(string); resourceType != "" {
					return resourceType
				}
			}
		}
	}
	parts := strings.Split(canonicalURL, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func profileURLFromResource(root map[string]any, mapProfileURL string) string {
	if meta, ok := root["meta"].(map[string]any); ok {
		profiles, _ := meta["profile"].([]any)
		for _, item := range profiles {
			if url, ok := item.(string); ok && strings.TrimSpace(url) != "" {
				return strings.TrimSpace(url)
			}
		}
	}
	return mapProfileURL
}

func baseStructureDefinitionURL(resourceType string) string {
	return "http://hl7.org/fhir/StructureDefinition/" + resourceType
}

func structureDefinitionElements(sd map[string]any) []any {
	derivation, _ := sd["derivation"].(string)
	differential, _ := sd["differential"].(map[string]any)
	snapshot, _ := sd["snapshot"].(map[string]any)
	diffElements, _ := differential["element"].([]any)
	snapElements, _ := snapshot["element"].([]any)
	switch {
	case derivation == "constraint" && len(diffElements) > 0:
		return diffElements
	case len(snapElements) > 0:
		return snapElements
	case len(diffElements) > 0:
		return diffElements
	default:
		return nil
	}
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
	switch strings.TrimSpace(max) {
	case "*":
		return true
	case "", "1", "0":
		return false
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
			if scoped, ok := resolver.(ResourceCardinalityResolver); ok {
				if repeating, ok := scoped.IsRepeatingFor(ctx, root, path); ok {
					return repeating
				}
			}
			if repeating, ok := resolver.IsRepeating(ctx, path); ok {
				return repeating
			}
		}
	}
	if len(elements) == 0 {
		return false
	}
	return isRepeatingDatatypeField(elements[len(elements)-1])
}
