package search

import (
	"sort"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

// CompositeComponentInfo describes one component of a composite SearchParameter.
type CompositeComponentInfo struct {
	Definition string
	Expression string
	Code       string
	Type       string
}

// ParameterInfo is compiled search parameter metadata used by haistack-search.
type ParameterInfo struct {
	CanonicalURL string
	Version      string
	Code         string
	Name         string
	Type         string
	Expression   string
	Target       []string
	Component    []CompositeComponentInfo
}

// Registry resolves enabled SearchParameters for resource types.
type Registry interface {
	IsResourceEnabled(resourceType string) bool
	SearchParametersFor(resourceType string) []ParameterInfo
	SearchParameter(resourceType, code string) (ParameterInfo, bool)
	HasSearchParameter(resourceType, code string) bool
	EnabledResourceTypes() []string
	ResolveComponentCode(canonicalURL string) (ParameterInfo, bool)
}

// SnapshotRegistry adapts a registry.Snapshot for search indexing and query resolution.
type SnapshotRegistry struct {
	snapshot *registry.Snapshot
}

// NewSnapshotRegistry wraps a compiled registry snapshot.
func NewSnapshotRegistry(snapshot *registry.Snapshot) *SnapshotRegistry {
	return &SnapshotRegistry{snapshot: snapshot}
}

func (r *SnapshotRegistry) IsResourceEnabled(resourceType string) bool {
	if r == nil || r.snapshot == nil {
		return false
	}
	return r.snapshot.IsResourceEnabled(resourceType)
}

func (r *SnapshotRegistry) SearchParametersFor(resourceType string) []ParameterInfo {
	if r == nil || r.snapshot == nil {
		return nil
	}
	params := r.snapshot.SearchParametersFor(resourceType)
	out := make([]ParameterInfo, 0, len(params))
	for _, p := range params {
		out = append(out, toParameterInfo(r.snapshot, p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *SnapshotRegistry) SearchParameter(resourceType, code string) (ParameterInfo, bool) {
	if r == nil || r.snapshot == nil {
		return ParameterInfo{}, false
	}
	info, ok := r.snapshot.SearchParameter(resourceType, code)
	if !ok {
		return ParameterInfo{}, false
	}
	return toParameterInfo(r.snapshot, *info), true
}

func (r *SnapshotRegistry) EnabledResourceTypes() []string {
	if r == nil || r.snapshot == nil {
		return nil
	}
	types := make([]string, 0)
	for _, cap := range r.snapshot.CapabilitySnapshot().Resources {
		types = append(types, cap.ResourceType)
	}
	sort.Strings(types)
	return types
}

func (r *SnapshotRegistry) HasSearchParameter(resourceType, code string) bool {
	if r == nil || r.snapshot == nil {
		return false
	}
	_, ok := r.snapshot.SearchParameter(resourceType, code)
	return ok
}

func (r *SnapshotRegistry) ResolveComponentCode(canonicalURL string) (ParameterInfo, bool) {
	if r == nil || r.snapshot == nil {
		return ParameterInfo{}, false
	}
	for _, cap := range r.snapshot.CapabilitySnapshot().Resources {
		for _, p := range cap.SearchParameters {
			if p.CanonicalURL == canonicalURL {
				return toParameterInfo(r.snapshot, p), true
			}
		}
	}
	return ParameterInfo{}, false
}

func toParameterInfo(snapshot *registry.Snapshot, p registry.SearchParameterInfo) ParameterInfo {
	components := make([]CompositeComponentInfo, 0, len(p.Component))
	for _, c := range p.Component {
		comp := CompositeComponentInfo{
			Definition: c.Definition,
			Expression: c.Expression,
		}
		if info, ok := lookupByCanonical(snapshot, c.Definition); ok {
			comp.Code = info.Code
			comp.Type = info.Type
		}
		components = append(components, comp)
	}
	return ParameterInfo{
		CanonicalURL: p.CanonicalURL,
		Version:      p.Version,
		Code:         p.Code,
		Name:         p.Name,
		Type:         p.Type,
		Expression:   p.Expression,
		Target:       append([]string(nil), p.Target...),
		Component:    components,
	}
}

func lookupByCanonical(snapshot *registry.Snapshot, canonicalURL string) (registry.SearchParameterInfo, bool) {
	for _, cap := range snapshot.CapabilitySnapshot().Resources {
		for _, p := range cap.SearchParameters {
			if p.CanonicalURL == canonicalURL {
				return p, true
			}
		}
	}
	return registry.SearchParameterInfo{}, false
}

func isSearchableType(paramType string) bool {
	switch paramType {
	case "token", "string", "date", "reference", "number", "composite", "quantity":
		return true
	default:
		return false
	}
}
