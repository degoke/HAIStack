package terminology

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// MemoryStore is useful for embedded runtimes and tests; its data model mirrors SQL stores.
type MemoryStore struct {
	mu        sync.RWMutex
	resources map[string]store.TerminologyResourceRecord
	concepts  map[string]store.TerminologyConceptRecord
	valuesets map[string]store.TerminologyValueSetRecord
	members   map[string][]store.TerminologyExpansionMemberRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{resources: map[string]store.TerminologyResourceRecord{}, concepts: map[string]store.TerminologyConceptRecord{}, valuesets: map[string]store.TerminologyValueSetRecord{}, members: map[string][]store.TerminologyExpansionMemberRecord{}}
}
func key(parts ...string) string {
	var x string
	for _, p := range parts {
		x += fmt.Sprintf("%d:%s|", len(p), p)
	}
	return x
}
func (m *MemoryStore) FindResource(_ context.Context, scope, typ, url, ver string) (*store.TerminologyResourceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.resources[key(scope, typ, url, ver)]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *MemoryStore) PutResource(_ context.Context, r store.TerminologyResourceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources[key(r.ScopeID, r.ResourceType, r.CanonicalURL, r.Version)] = r
	return nil
}
func (m *MemoryStore) DeleteResource(_ context.Context, scope, typ, url, ver string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.resources, key(scope, typ, url, ver))
	return nil
}
func (m *MemoryStore) ListResources(_ context.Context, scope, typ string) ([]store.TerminologyResourceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []store.TerminologyResourceRecord
	for _, r := range m.resources {
		if r.ScopeID == scope && (typ == "" || r.ResourceType == typ) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CanonicalURL+out[i].Version < out[j].CanonicalURL+out[j].Version })
	return out, nil
}
func (m *MemoryStore) ReplaceCodeSystem(_ context.Context, scope, url, ver string, cs []store.TerminologyConceptRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, c := range m.concepts {
		if c.ScopeID == scope && c.SystemURL == url && c.SystemVersion == ver {
			delete(m.concepts, k)
		}
	}
	for _, c := range cs {
		m.concepts[key(scope, url, ver, c.Code)] = c
	}
	return nil
}
func (m *MemoryStore) LookupConcept(_ context.Context, scope, url, ver, code string) (*store.TerminologyConceptRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ver != "" {
		c, ok := m.concepts[key(scope, url, ver, code)]
		if !ok {
			return nil, nil
		}
		return &c, nil
	}
	var found *store.TerminologyConceptRecord
	for _, c := range m.concepts {
		r, resourceOK := m.resources[key(scope, "CodeSystem", url, c.SystemVersion)]
		if c.ScopeID == scope && c.SystemURL == url && c.Code == code && (found == nil || c.SystemVersion > found.SystemVersion) && c.Active && (!resourceOK || r.Status != "retired") {
			cc := c
			found = &cc
		}
	}
	return found, nil
}
func (m *MemoryStore) ReplaceValueSet(_ context.Context, r store.TerminologyValueSetRecord, ms []store.TerminologyExpansionMemberRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.valuesets[key(r.ScopeID, r.CanonicalURL, r.Version)] = r
	m.members[key(r.ScopeID, r.CanonicalURL, r.Version)] = append([]store.TerminologyExpansionMemberRecord(nil), ms...)
	return nil
}
func (m *MemoryStore) GetValueSet(_ context.Context, scope, url, ver string) (*store.TerminologyValueSetRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ver != "" {
		r, ok := m.valuesets[key(scope, url, ver)]
		if !ok {
			return nil, nil
		}
		return &r, nil
	}
	var found *store.TerminologyValueSetRecord
	for _, r := range m.valuesets {
		if r.ScopeID == scope && r.CanonicalURL == url && (found == nil || r.Version > found.Version) && r.Status != "retired" {
			x := r
			found = &x
		}
	}
	return found, nil
}
func (m *MemoryStore) ListValueSetMembers(_ context.Context, scope, url, ver string) ([]store.TerminologyExpansionMemberRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]store.TerminologyExpansionMemberRecord(nil), m.members[key(scope, url, ver)]...), nil
}
func (m *MemoryStore) DeleteProjections(_ context.Context, scope, typ, url, ver string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if typ == "CodeSystem" {
		for k, c := range m.concepts {
			if c.ScopeID == scope && c.SystemURL == url && c.SystemVersion == ver {
				delete(m.concepts, k)
			}
		}
	}
	if typ == "ValueSet" {
		delete(m.valuesets, key(scope, url, ver))
		delete(m.members, key(scope, url, ver))
	}
	return nil
}
