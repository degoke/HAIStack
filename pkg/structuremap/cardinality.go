package structuremap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/degoke/health-ai-stack/pkg/registry"
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
	// authoritative profiles use snapshot elements only and do not fall back to base paths.
	authoritative map[string]bool
}

func newMapCardinalityResolver(store store.DefinitionStore, m Map) *mapCardinalityResolver {
	return &mapCardinalityResolver{
		store:         store,
		profiles:      targetProfilesFromMap(context.Background(), store, m),
		indexes:       map[string]map[string]string{},
		authoritative: map[string]bool{},
	}
}

func (e Engine) cardinalityForMap(m Map) CardinalityResolver {
	base := e.Cardinality
	if base == nil {
		base = &StoreCardinalityResolver{Store: registry.DefinitionStoreWithEmbeddedBase(nil)}
	}
	if storeResolver, ok := base.(*StoreCardinalityResolver); ok && storeResolver != nil && storeResolver.Store != nil {
		return newMapCardinalityResolver(storeResolver.Store, m)
	}
	return base
}

func (r *mapCardinalityResolver) IsRepeating(ctx context.Context, fhirPath string) (bool, bool) {
	return r.lookup(ctx, "", fhirPath)
}

func (r *mapCardinalityResolver) IsRepeatingFor(ctx context.Context, root map[string]any, fhirPath string) (bool, bool) {
	resourceType, _, ok := splitFHIRElementPath(fhirPath)
	if !ok {
		return false, false
	}
	profileURLs := profileURLsFromResource(root, r.profiles[resourceType])
	lookups := make([]profileCardinalityLookup, len(profileURLs))
	for i, profileURL := range profileURLs {
		repeating, resolved, err := r.lookupResolved(ctx, profileURL, fhirPath)
		lookups[i] = profileCardinalityLookup{
			authoritative: r.isSnapshotAuthoritative(ctx, profileURL),
			repeating:     repeating,
			resolved:      resolved,
			err:           err,
		}
	}
	for _, lookup := range lookups {
		if lookup.err != nil {
			return false, false
		}
		if lookup.resolved && !lookup.repeating {
			return false, true
		}
	}
	for _, lookup := range lookups {
		if lookup.resolved && lookup.repeating {
			return true, true
		}
	}
	for _, lookup := range lookups {
		if lookup.authoritative {
			return false, false
		}
	}
	return r.lookup(ctx, "", fhirPath)
}

type profileCardinalityLookup struct {
	authoritative bool
	repeating     bool
	resolved      bool
	err           error
}

func (r *mapCardinalityResolver) lookup(ctx context.Context, profileURL, fhirPath string) (bool, bool) {
	repeating, resolved, err := r.lookupResolved(ctx, profileURL, fhirPath)
	if err != nil || !resolved {
		return false, false
	}
	return repeating, true
}

func (r *mapCardinalityResolver) lookupResolved(ctx context.Context, profileURL, fhirPath string) (bool, bool, error) {
	resourceType, elementPath, ok := splitFHIRElementPath(fhirPath)
	if !ok {
		return false, false, nil
	}
	if profileURL == "" {
		profileURL = r.profiles[resourceType]
	}
	if profileURL != "" {
		max, found, err := r.elementMax(ctx, profileURL, resourceType, elementPath)
		if err != nil {
			return false, false, err
		}
		if found {
			return isRepeatingMax(max), true, nil
		}
		if r.isSnapshotAuthoritative(ctx, profileURL) {
			return false, false, nil
		}
	}
	baseCanonical := baseStructureDefinitionURL(resourceType)
	max, found, err := r.elementMax(ctx, baseCanonical, resourceType, elementPath)
	if err != nil {
		return false, false, err
	}
	if found {
		return isRepeatingMax(max), true, nil
	}
	return false, false, nil
}

func (r *mapCardinalityResolver) elementMax(ctx context.Context, canonicalURL, resourceType, elementPath string) (string, bool, error) {
	index, err := r.loadIndex(ctx, canonicalURL, resourceType)
	if err != nil {
		return "", false, err
	}
	max, ok := index[elementPath]
	if !ok {
		max, ok = indexSliceAlias(index, elementPath)
	}
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
	if cached, ok := r.indexes[canonicalURL]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	index, authoritative, err := r.buildIndex(ctx, canonicalURL, resourceType)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if cached, ok := r.indexes[canonicalURL]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.indexes[canonicalURL] = index
	r.authoritative[canonicalURL] = authoritative
	r.mu.Unlock()
	return index, nil
}

func (r *mapCardinalityResolver) isSnapshotAuthoritative(ctx context.Context, canonicalURL string) bool {
	r.mu.Lock()
	if authoritative, ok := r.authoritative[canonicalURL]; ok {
		r.mu.Unlock()
		return authoritative
	}
	r.mu.Unlock()

	_, authoritative, err := r.buildIndex(ctx, canonicalURL, "")
	if err != nil {
		return false
	}
	return authoritative
}

func (r *mapCardinalityResolver) buildIndex(ctx context.Context, canonicalURL, resourceType string) (map[string]string, bool, error) {
	record, err := r.store.Get(ctx, canonicalURL, "")
	if err != nil || record == nil || len(record.JSONData) == 0 {
		return nil, false, fmt.Errorf("StructureDefinition %s not found", canonicalURL)
	}
	var sd map[string]any
	if err := json.Unmarshal(record.JSONData, &sd); err != nil {
		return nil, false, fmt.Errorf("parse StructureDefinition %s: %w", canonicalURL, err)
	}
	if resourceType == "" {
		resourceType, _ = sd["type"].(string)
	}
	index := elementIndexFromDefinition(sd, resourceType)
	sdType, _ := sd["type"].(string)
	if sdType == "" {
		sdType = resourceType
	}
	baseCanonical := baseStructureDefinitionURL(sdType)
	if canonicalURL != baseCanonical && profileUsesDifferentialOverlay(sd) {
		baseIndex, err := r.loadIndex(ctx, baseCanonical, sdType)
		if err == nil {
			index = mergeElementIndexes(baseIndex, index)
		}
	}
	return index, profileIsSnapshotAuthoritative(sd), nil
}

func profileUsesDifferentialOverlay(sd map[string]any) bool {
	derivation, _ := sd["derivation"].(string)
	if derivation != "constraint" {
		return false
	}
	differential, _ := sd["differential"].(map[string]any)
	diffElements, _ := differential["element"].([]any)
	if len(diffElements) == 0 {
		return false
	}
	snapshot, _ := sd["snapshot"].(map[string]any)
	snapElements, _ := snapshot["element"].([]any)
	return len(snapElements) == 0
}

func profileIsSnapshotAuthoritative(sd map[string]any) bool {
	snapshot, _ := sd["snapshot"].(map[string]any)
	snapElements, _ := snapshot["element"].([]any)
	if len(snapElements) == 0 {
		return false
	}
	return !profileUsesDifferentialOverlay(sd)
}

func elementIndexFromDefinition(sd map[string]any, resourceType string) map[string]string {
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
		sliceName, _ := element["sliceName"].(string)
		if sliceName != "" {
			index[path+":"+sliceName] = max
		} else {
			index[path] = max
		}
		if strings.Contains(path, "[x]") {
			parent, child := splitElementPath(path)
			if parent != "" && child != "" {
				types := elementTypes(element)
				for _, key := range choiceJSONKeys(child, types) {
					index[parent+"."+key] = max
				}
			}
		}
	}
	return index
}

func elementTypes(element map[string]any) []string {
	raw, _ := element["type"].([]any)
	types := make([]string, 0, len(raw))
	for _, item := range raw {
		typed, _ := item.(map[string]any)
		if typed == nil {
			continue
		}
		if code, _ := typed["code"].(string); code != "" {
			types = append(types, code)
		}
	}
	return types
}

func splitElementPath(path string) (parent, child string) {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i+1:]
}

func indexSliceAlias(index map[string]string, elementPath string) (string, bool) {
	idx := strings.Index(elementPath, ":")
	if idx <= 0 {
		return "", false
	}
	max, ok := index[elementPath[:idx]]
	return max, ok
}

func choiceJSONKeys(choiceName string, types []string) []string {
	base := strings.TrimSuffix(choiceName, "[x]")
	if base == "" || len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, typ := range types {
		if typ == "" {
			continue
		}
		out = append(out, base+choiceTypeJSONSuffix(typ))
	}
	return out
}

func choiceTypeJSONSuffix(typ string) string {
	if suffix, ok := choiceTypeJSONSuffixes[typ]; ok {
		return suffix
	}
	runes := []rune(typ)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

var choiceTypeJSONSuffixes = map[string]string{
	"uri":            "Uri",
	"url":            "Url",
	"uuid":           "Uuid",
	"oid":            "Oid",
	"id":             "Id",
	"dateTime":       "DateTime",
	"instant":        "Instant",
	"time":           "Time",
	"date":           "Date",
	"positiveInt":    "PositiveInt",
	"unsignedInt":    "UnsignedInt",
	"base64Binary":   "Base64Binary",
	"xhtml":          "Xhtml",
	"markdown":       "Markdown",
	"canonical":      "Canonical",
}

func mergeElementIndexes(base, overlay map[string]string) map[string]string {
	if len(base) == 0 {
		return overlay
	}
	merged := map[string]string{}
	for path, max := range base {
		merged[path] = max
	}
	for path, max := range overlay {
		merged[path] = max
	}
	return merged
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

func profileURLsFromResource(root map[string]any, mapProfileURL string) []string {
	var urls []string
	if meta, ok := root["meta"].(map[string]any); ok {
		profiles, _ := meta["profile"].([]any)
		for _, item := range profiles {
			if url, ok := item.(string); ok {
				url = strings.TrimSpace(url)
				if url != "" {
					urls = append(urls, url)
				}
			}
		}
	}
	if mapProfileURL != "" && !containsString(urls, mapProfileURL) {
		urls = append(urls, mapProfileURL)
	}
	return urls
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
	case len(snapElements) > 0:
		return snapElements
	case derivation == "constraint" && len(diffElements) > 0:
		return diffElements
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
