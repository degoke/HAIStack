package smart

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ErrScopeFilterDenied is returned when a resource or search request falls outside
// SMART 2.2 scope filters.
var ErrScopeFilterDenied = fmt.Errorf("%w: resource outside granted scope filters", auth.ErrDenied)

// ApplyScopeFiltersToParams intersects SMART 2.2 scope filters with FHIR search params.
func ApplyScopeFiltersToParams(scopes ScopeSet, actor ActorClass, resourceType string, params url.Values) (url.Values, error) {
	if scopes.Empty() {
		return params, nil
	}
	applicable := scopes.ScopesAllowingOp(actor, resourceType, OpSearch)
	if len(applicable) == 0 {
		return params, nil
	}
	out := auth.CloneURLValues(params)
	for _, sc := range applicable {
		if len(sc.Filters) == 0 {
			continue
		}
		for key, vals := range sc.Filters {
			existing := out[key]
			if len(existing) == 0 {
				out[key] = append([]string(nil), vals...)
				continue
			}
			out[key] = intersectValues(existing, vals)
			if len(out[key]) == 0 {
				return nil, ErrScopeFilterDenied
			}
		}
	}
	return out, nil
}

// CheckEnvelopeScopeFilters enforces SMART 2.2 scope filters for a loaded resource.
func CheckEnvelopeScopeFilters(scopes ScopeSet, actor ActorClass, resourceType string, op AccessOp, resource *types.ResourceEnvelope) error {
	if scopes.Empty() || resource == nil {
		return nil
	}
	if scopes.AllowsResourceWithFilters(actor, resourceType, op, resource) {
		return nil
	}
	return ErrScopeFilterDenied
}

// FilterSearchBundleScopeFilters removes bundle entries outside granted scope filters.
func FilterSearchBundleScopeFilters(scopes ScopeSet, actor ActorClass, resourceType string, bundle *search.SearchBundle) error {
	if bundle == nil || scopes.Empty() {
		return nil
	}
	kept := make([]search.BundleEntry, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		if entry.Resource == nil {
			continue
		}
		resType := entry.Resource.ResourceType
		if resType == "" {
			resType = resourceType
		}
		if err := CheckEnvelopeScopeFilters(scopes, actor, resType, OpSearch, entry.Resource); err != nil {
			if err == ErrScopeFilterDenied {
				continue
			}
			return err
		}
		kept = append(kept, entry)
	}
	bundle.Entries = kept
	bundle.Count = len(kept)
	if bundle.Total != nil {
		total := len(kept)
		bundle.Total = &total
	}
	return nil
}

// AllowsResourceWithFilters reports whether any granted scope authorizes the resource
// and, when present, scope filters match the resource payload.
func (s ScopeSet) AllowsResourceWithFilters(actor ActorClass, resourceType string, op AccessOp, resource *types.ResourceEnvelope) bool {
	scopes := s.ScopesAllowingOp(actor, resourceType, op)
	if len(scopes) == 0 {
		return false
	}
	for _, sc := range scopes {
		if len(sc.Filters) == 0 {
			return true
		}
		if resourceMatchesScopeFilters(resourceType, resource, sc.Filters) {
			return true
		}
	}
	return false
}

// ActorForPrincipal returns the SMART actor class for a principal and scope set.
func ActorForPrincipal(kind auth.PrincipalKind, scopes ScopeSet) ActorClass {
	return actorForKind(kind, scopes)
}

func resourceMatchesScopeFilters(resourceType string, resource *types.ResourceEnvelope, filters url.Values) bool {
	if len(filters) == 0 {
		return true
	}
	for param, wantVals := range filters {
		if !resourceMatchesSearchParam(resourceType, resource, param, wantVals) {
			return false
		}
	}
	return true
}

func resourceMatchesSearchParam(resourceType string, resource *types.ResourceEnvelope, param string, want []string) bool {
	switch {
	case resourceType == "Observation" && param == "category":
		return envelopeHasCodeInField(resource, "category", want)
	default:
		if v, ok := resource.StringField(param); ok {
			for _, w := range want {
				if strings.EqualFold(v, w) {
					return true
				}
			}
			return false
		}
		return len(want) == 0
	}
}

func envelopeHasCodeInField(resource *types.ResourceEnvelope, field string, codes []string) bool {
	val, ok := resource.Field(field)
	if !ok {
		return false
	}
	want := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		want[strings.ToLower(code)] = struct{}{}
	}
	items, ok := val.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if code := codingCodeFromConcept(item); code != "" {
			if _, ok := want[strings.ToLower(code)]; ok {
				return true
			}
		}
	}
	return false
}

func codingCodeFromConcept(value any) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	codings, ok := obj["coding"].([]any)
	if !ok {
		return ""
	}
	for _, coding := range codings {
		codingObj, ok := coding.(map[string]any)
		if !ok {
			continue
		}
		if code, ok := codingObj["code"].(string); ok && code != "" {
			return code
		}
	}
	return ""
}

// FilterBundleEnvelopeScopeFilters removes bundle entries outside granted scope filters.
func FilterBundleEnvelopeScopeFilters(scopes ScopeSet, actor ActorClass, defaultResourceType string, envelope *types.ResourceEnvelope, codec types.ResourceCodec) (*types.ResourceEnvelope, error) {
	if envelope == nil || scopes.Empty() || envelope.ResourceType != "Bundle" {
		return envelope, nil
	}
	if codec == nil {
		codec = types.NewJSONCodec()
	}
	bundle, err := envelopeBundleToSearchBundle(envelope, codec)
	if err != nil {
		return nil, err
	}
	if err := FilterSearchBundleScopeFilters(scopes, actor, defaultResourceType, bundle); err != nil {
		return nil, err
	}
	return searchBundleToBundleEnvelope(bundle, envelope, codec)
}

func envelopeBundleToSearchBundle(envelope *types.ResourceEnvelope, codec types.ResourceCodec) (*search.SearchBundle, error) {
	var raw map[string]any
	if err := json.Unmarshal(envelope.JSON, &raw); err != nil {
		return nil, fmt.Errorf("parse bundle envelope: %w", err)
	}
	entriesRaw, _ := raw["entry"].([]any)
	bundle := &search.SearchBundle{ResourceType: "Bundle"}
	for _, item := range entriesRaw {
		entryObj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mode, _ := entryObj["search"].(map[string]any)
		modeStr := ""
		if mode != nil {
			modeStr, _ = mode["mode"].(string)
		}
		if modeStr == "" {
			modeStr, _ = entryObj["mode"].(string)
		}
		resourceObj, ok := entryObj["resource"].(map[string]any)
		if !ok {
			continue
		}
		resourceType, _ := resourceObj["resourceType"].(string)
		data, err := json.Marshal(resourceObj)
		if err != nil {
			return nil, err
		}
		parsed, err := codec.ParseJSON(resourceType, data)
		if err != nil {
			return nil, err
		}
		fullURL, _ := entryObj["fullUrl"].(string)
		bundle.Entries = append(bundle.Entries, search.BundleEntry{
			FullURL:  fullURL,
			Resource: parsed,
			Mode:     modeStr,
		})
	}
	bundle.Count = len(bundle.Entries)
	return bundle, nil
}

func searchBundleToBundleEnvelope(bundle *search.SearchBundle, original *types.ResourceEnvelope, codec types.ResourceCodec) (*types.ResourceEnvelope, error) {
	var raw map[string]any
	if err := json.Unmarshal(original.JSON, &raw); err != nil {
		return nil, err
	}
	entries := make([]map[string]any, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		if entry.Resource == nil {
			continue
		}
		item := map[string]any{}
		if entry.FullURL != "" {
			item["fullUrl"] = entry.FullURL
		}
		var resourceObj any
		if err := json.Unmarshal(entry.Resource.JSON, &resourceObj); err != nil {
			return nil, err
		}
		item["resource"] = resourceObj
		if entry.Mode != "" {
			item["search"] = map[string]any{"mode": entry.Mode}
		}
		entries = append(entries, item)
	}
	raw["entry"] = entries
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	out, err := codec.ParseJSON("Bundle", data)
	if err != nil {
		return nil, err
	}
	out.VersionID = original.VersionID
	out.LastUpdated = original.LastUpdated
	return out, nil
}

func intersectValues(existing, required []string) []string {
	want := make(map[string]struct{}, len(required))
	for _, v := range required {
		want[strings.ToLower(v)] = struct{}{}
	}
	var out []string
	for _, v := range existing {
		if _, ok := want[strings.ToLower(v)]; ok {
			out = append(out, v)
		}
	}
	return out
}
