package registry

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// DefinitionStoreWithEmbeddedBase returns a store that resolves from primary first
// and falls back to bundled HL7 R4 StructureDefinitions.
func DefinitionStoreWithEmbeddedBase(primary store.DefinitionStore) store.DefinitionStore {
	embedded := EmbeddedDefinitionStore()
	if primary == nil {
		return embedded
	}
	return &chainedDefinitionStore{primary: primary, fallback: embedded}
}

type chainedDefinitionStore struct {
	primary  store.DefinitionStore
	fallback store.DefinitionStore
}

func (s *chainedDefinitionStore) Upsert(ctx context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	if s.primary != nil {
		return s.primary.Upsert(ctx, record, targets)
	}
	return nil
}

func (s *chainedDefinitionStore) Delete(ctx context.Context, canonicalURL, version string) error {
	if s.primary != nil {
		return s.primary.Delete(ctx, canonicalURL, version)
	}
	return nil
}

func (s *chainedDefinitionStore) List(ctx context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	if s.primary != nil {
		return s.primary.List(ctx, filter)
	}
	return nil, nil
}

func (s *chainedDefinitionStore) Get(ctx context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	if s.primary != nil {
		record, err := s.primary.Get(ctx, canonicalURL, version)
		if err == nil && record != nil && len(record.JSONData) > 0 {
			return record, nil
		}
	}
	if s.fallback == nil {
		return nil, context.Canceled
	}
	return s.fallback.Get(ctx, canonicalURL, version)
}
