package terminology

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// LayeredStore composes a tenant terminology overlay with a global canonical
// catalog. ValueSets and tenant-local CodeSystems live in the tenant scope;
// global CodeSystem projections are consulted as a fallback for lookups and
// composition.
type LayeredStore struct {
	Store         store.TerminologyStore
	TenantScopeID string
	GlobalScopeID string
}

// NewLayeredStore constructs a tenant-facing terminology store view.
func NewLayeredStore(st store.TerminologyStore, tenantScopeID string) *LayeredStore {
	return &LayeredStore{Store: st, TenantScopeID: tenantScopeID, GlobalScopeID: GlobalScopeID}
}

func (l *LayeredStore) FindResource(ctx context.Context, scope, typ, url, ver string) (*store.TerminologyResourceRecord, error) {
	if typ == "ValueSet" {
		return l.Store.FindResource(ctx, l.tenantScope(scope), typ, url, ver)
	}
	r, err := l.Store.FindResource(ctx, l.tenantScope(scope), typ, url, ver)
	if err != nil || r != nil || l.GlobalScopeID == "" {
		return r, err
	}
	return l.Store.FindResource(ctx, l.GlobalScopeID, typ, url, ver)
}

func (l *LayeredStore) PutResource(ctx context.Context, record store.TerminologyResourceRecord) error {
	scope := l.writeScope(record.ResourceType, record.ScopeID)
	record.ScopeID = scope
	return l.Store.PutResource(ctx, record)
}

func (l *LayeredStore) DeleteResource(ctx context.Context, scope, typ, url, ver string) error {
	if typ == "ValueSet" {
		return l.Store.DeleteResource(ctx, l.tenantScope(scope), typ, url, ver)
	}
	if err := l.Store.DeleteResource(ctx, l.tenantScope(scope), typ, url, ver); err != nil {
		return err
	}
	if l.GlobalScopeID == "" {
		return nil
	}
	return l.Store.DeleteResource(ctx, l.GlobalScopeID, typ, url, ver)
}

func (l *LayeredStore) ListResources(ctx context.Context, scope, typ string) ([]store.TerminologyResourceRecord, error) {
	if typ == "ValueSet" {
		return l.Store.ListResources(ctx, l.tenantScope(scope), typ)
	}
	tenant, err := l.Store.ListResources(ctx, l.tenantScope(scope), typ)
	if err != nil {
		return nil, err
	}
	if l.GlobalScopeID == "" || typ != "" && typ != "CodeSystem" {
		return tenant, nil
	}
	global, err := l.Store.ListResources(ctx, l.GlobalScopeID, typ)
	if err != nil {
		return nil, err
	}
	return mergeResources(tenant, global), nil
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
	if err != nil || c != nil || l.GlobalScopeID == "" {
		return c, err
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
	return l.Store.GetValueSet(ctx, l.tenantScope(scope), url, ver)
}

func (l *LayeredStore) ListValueSetMembers(ctx context.Context, scope, url, ver string) ([]store.TerminologyExpansionMemberRecord, error) {
	return l.Store.ListValueSetMembers(ctx, l.tenantScope(scope), url, ver)
}

func (l *LayeredStore) DeleteProjections(ctx context.Context, scope, typ, url, ver string) error {
	if typ == "ValueSet" {
		return l.Store.DeleteProjections(ctx, l.tenantScope(scope), typ, url, ver)
	}
	if err := l.Store.DeleteProjections(ctx, l.tenantScope(scope), typ, url, ver); err != nil {
		return err
	}
	if l.GlobalScopeID == "" {
		return nil
	}
	return l.Store.DeleteProjections(ctx, l.GlobalScopeID, typ, url, ver)
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
