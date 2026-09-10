package conceptmap

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// TerminologyStoreResolver resolves ConceptMaps from a terminology store projection.
type TerminologyStoreResolver struct {
	Store   store.TerminologyStore
	ScopeID string
}

func (r *TerminologyStoreResolver) Resolve(ctx context.Context, canonical string) (Map, error) {
	if r == nil || r.Store == nil {
		return Map{}, ErrNotFound(canonical)
	}
	url, version := splitCanonical(canonical)
	record, err := r.Store.FindResource(ctx, r.ScopeID, "ConceptMap", url, version)
	if err != nil {
		return Map{}, err
	}
	if record == nil || len(record.ResourceJSON) == 0 {
		return Map{}, ErrNotFound(canonical)
	}
	m, err := ParseMap(record.ResourceJSON)
	if err != nil {
		return Map{}, err
	}
	if version != "" && !matchesCanonical(m, canonical) {
		return Map{}, ErrNotFound(canonical)
	}
	return m, nil
}

