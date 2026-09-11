package terminology

import (
	"context"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// LayeredStore composes a tenant terminology overlay with a global canonical
// catalog. ValueSets and tenant-local CodeSystems live in the tenant scope;
// global CodeSystem projections are consulted as a fallback for lookups and
// composition when the tenant has opted in via TerminologyInstallStore.
type LayeredStore struct {
	Store         store.TerminologyStore
	TenantScopeID string
	GlobalScopeID string
	Installs      store.TerminologyInstallStore
}

// NewLayeredStore constructs a tenant-facing terminology store view.
func NewLayeredStore(st store.TerminologyStore, tenantScopeID string) *LayeredStore {
	return &LayeredStore{Store: st, TenantScopeID: tenantScopeID, GlobalScopeID: GlobalScopeID}
}

func (l *LayeredStore) FindResource(ctx context.Context, scope, typ, url, ver string) (*store.TerminologyResourceRecord, error) {
	r, err := l.Store.FindResource(ctx, l.tenantScope(scope), typ, url, ver)
	if err != nil || r != nil {
		return r, err
	}
	if typ != "CodeSystem" && typ != "ValueSet" {
		return nil, nil
	}
	if !l.globalAllowed(ctx, url, ver, typ) {
		return nil, nil
	}
	return l.Store.FindResource(ctx, l.GlobalScopeID, typ, url, ver)
}

func (l *LayeredStore) PutResource(ctx context.Context, record store.TerminologyResourceRecord) error {
	scope := l.writeScope(record.ResourceType, record.ScopeID)
	record.ScopeID = scope
	return l.Store.PutResource(ctx, record)
}

func (l *LayeredStore) DeleteResource(ctx context.Context, scope, typ, url, ver string) error {
	// Tenant overlay only; global CodeSystem deletes are explicit admin operations.
	return l.Store.DeleteResource(ctx, l.tenantScope(scope), typ, url, ver)
}

func (l *LayeredStore) ListResources(ctx context.Context, scope, typ string) ([]store.TerminologyResourceRecord, error) {
	tenant, err := l.Store.ListResources(ctx, l.tenantScope(scope), typ)
	if err != nil {
		return nil, err
	}
	if l.GlobalScopeID == "" || (typ != "" && typ != "CodeSystem" && typ != "ValueSet") {
		return tenant, nil
	}
	global, err := l.Store.ListResources(ctx, l.GlobalScopeID, typ)
	if err != nil {
		return nil, err
	}
	enabled, err := l.enabledKeys(ctx, typ)
	if err != nil {
		return nil, err
	}
	filtered := make([]store.TerminologyResourceRecord, 0, len(global))
	for _, r := range global {
		if enabled[installKey(r.ResourceType, r.CanonicalURL, r.Version)] {
			filtered = append(filtered, r)
		}
	}
	return mergeResources(tenant, filtered), nil
}

func (l *LayeredStore) ReplaceCodeSystem(ctx context.Context, scope, url, ver string, concepts []store.TerminologyConceptRecord) error {
	writeScope := l.writeScope("CodeSystem", scope)
	for i := range concepts {
		concepts[i].ScopeID = writeScope
	}
	return l.Store.ReplaceCodeSystem(ctx, writeScope, url, ver, concepts)
}

func (l *LayeredStore) LookupConcept(ctx context.Context, scope, url, ver, code string) (*store.TerminologyConceptRecord, error) {
	c, err := l.Store.LookupConcept(ctx, l.tenantScope(scope), url, ver, code)
	if err != nil || c != nil {
		return c, err
	}
	if !l.globalAllowed(ctx, url, ver, "CodeSystem") {
		return nil, nil
	}
	return l.Store.LookupConcept(ctx, l.GlobalScopeID, url, ver, code)
}

func (l *LayeredStore) ReplaceValueSet(ctx context.Context, record store.TerminologyValueSetRecord, members []store.TerminologyExpansionMemberRecord) error {
	record.ScopeID = l.tenantScope(record.ScopeID)
	for i := range members {
		members[i].ScopeID = record.ScopeID
	}
	return l.Store.ReplaceValueSet(ctx, record, members)
}

func (l *LayeredStore) GetValueSet(ctx context.Context, scope, url, ver string) (*store.TerminologyValueSetRecord, error) {
	v, err := l.Store.GetValueSet(ctx, l.tenantScope(scope), url, ver)
	if err != nil || v != nil {
		return v, err
	}
	if !l.globalAllowed(ctx, url, ver, "ValueSet") {
		return nil, nil
	}
	return l.Store.GetValueSet(ctx, l.GlobalScopeID, url, ver)
}

func (l *LayeredStore) ListValueSetMembers(ctx context.Context, scope, url, ver string) ([]store.TerminologyExpansionMemberRecord, error) {
	ms, err := l.Store.ListValueSetMembers(ctx, l.tenantScope(scope), url, ver)
	if err != nil || len(ms) > 0 {
		return ms, err
	}
	if !l.globalAllowed(ctx, url, ver, "ValueSet") {
		return nil, nil
	}
	return l.Store.ListValueSetMembers(ctx, l.GlobalScopeID, url, ver)
}

func (l *LayeredStore) DeleteProjections(ctx context.Context, scope, typ, url, ver string) error {
	return l.Store.DeleteProjections(ctx, l.tenantScope(scope), typ, url, ver)
}

func (l *LayeredStore) tenantScope(scope string) string {
	if scope != "" {
		return scope
	}
	return l.TenantScopeID
}

func (l *LayeredStore) writeScope(resourceType, scope string) string {
	if resourceType == "CodeSystem" && l.GlobalScopeID != "" {
		return l.GlobalScopeID
	}
	return l.tenantScope(scope)
}

func (l *LayeredStore) globalAllowed(ctx context.Context, url, ver, resourceType string) bool {
	if l.GlobalScopeID == "" {
		return false
	}
	if l.Installs == nil {
		return false
	}
	enabled, err := l.enabledKeys(ctx, resourceType)
	if err != nil {
		return false
	}
	if ver != "" {
		return enabled[installKey(resourceType, url, ver)]
	}
	prefix := resourceType + "|" + url + "|"
	for key := range enabled {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (l *LayeredStore) enabledKeys(ctx context.Context, resourceType string) (map[string]bool, error) {
	if l.Installs == nil {
		return nil, nil
	}
	rows, err := l.Installs.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		typ := row.ResourceType
		if typ == "" {
			typ = "CodeSystem"
		}
		if resourceType != "" && typ != resourceType {
			continue
		}
		out[installKey(typ, row.CanonicalURL, row.Version)] = true
	}
	return out, nil
}

func installKey(resourceType, url, version string) string {
	if resourceType == "" {
		resourceType = "CodeSystem"
	}
	return resourceType + "|" + url + "|" + version
}

func mergeResources(tenant, global []store.TerminologyResourceRecord) []store.TerminologyResourceRecord {
	seen := make(map[string]store.TerminologyResourceRecord, len(tenant)+len(global))
	for _, r := range tenant {
		seen[r.CanonicalURL+"|"+r.Version] = r
	}
	for _, r := range global {
		key := r.CanonicalURL + "|" + r.Version
		if _, ok := seen[key]; !ok {
			seen[key] = r
		}
	}
	out := make([]store.TerminologyResourceRecord, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	return out
}
