package search

import (
	"sort"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

// ParameterInfo is compiled search parameter metadata used by haistack-search.
type ParameterInfo struct {
	Code       string
	Name       string
	Type       string
	Expression string
}

// Registry resolves enabled SearchParameters for resource types.
type Registry interface {
	IsResourceEnabled(resourceType string) bool
	SearchParametersFor(resourceType string) []ParameterInfo
	SearchParameter(resourceType, code string) (ParameterInfo, bool)
	HasSearchParameter(resourceType, code string) bool
	EnabledResourceTypes() []string
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
		if !isSupportedParam(p.Code) {
			continue
		}
		out = append(out, toParameterInfo(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *SnapshotRegistry) SearchParameter(resourceType, code string) (ParameterInfo, bool) {
	if r == nil || r.snapshot == nil || !isSupportedParam(code) {
		return ParameterInfo{}, false
	}
	info, ok := r.snapshot.SearchParameter(resourceType, code)
	if !ok {
		return ParameterInfo{}, false
	}
	return toParameterInfo(*info), true
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
	_, ok := r.lookupRawSearchParameter(resourceType, code)
	return ok
}

func (r *SnapshotRegistry) lookupRawSearchParameter(resourceType, code string) (ParameterInfo, bool) {
	if r == nil || r.snapshot == nil {
		return ParameterInfo{}, false
	}
	info, ok := r.snapshot.SearchParameter(resourceType, code)
	if !ok {
		return ParameterInfo{}, false
	}
	return toParameterInfo(*info), true
}

func toParameterInfo(p registry.SearchParameterInfo) ParameterInfo {
	return ParameterInfo{
		Code:       p.Code,
		Name:       p.Name,
		Type:       p.Type,
		Expression: p.Expression,
	}
}

// supportedParams is the set of search parameter codes implemented by haistack-search.
var supportedParams = map[string]struct{}{
	"_id":          {},
	"_lastUpdated": {},
	"identifier":   {},
	"name":         {},
	"phone":        {},
	"birthdate":    {},
	"patient":      {},
	"subject":      {},
	"encounter":    {},
	"status":       {},
	"date":         {},
	"code":         {},
}

func isSupportedParam(code string) bool {
	_, ok := supportedParams[code]
	return ok
}
