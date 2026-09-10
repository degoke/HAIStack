package registry

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// EmbeddedDefinitionStore resolves HL7 R4 base StructureDefinitions from the embedded bundle.
func EmbeddedDefinitionStore() store.DefinitionStore {
	return embeddedDefinitionStoreInst
}

var embeddedDefinitionStoreInst = &embeddedDefinitionStore{}

type embeddedDefinitionStore struct {
	mu      sync.Mutex
	byURL   map[string][]byte
	loaded  bool
}

func (s *embeddedDefinitionStore) Upsert(context.Context, store.DefinitionResourceRecord, []store.DefinitionTargetRecord) error {
	return nil
}

func (s *embeddedDefinitionStore) Delete(context.Context, string, string) error {
	return nil
}

func (s *embeddedDefinitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	return nil, nil
}

func (s *embeddedDefinitionStore) Get(_ context.Context, canonicalURL, _ string) (*store.DefinitionResourceRecord, error) {
	s.ensureLoaded()
	raw, ok := s.byURL[canonicalURL]
	if !ok || len(raw) == 0 {
		return nil, context.Canceled
	}
	return &store.DefinitionResourceRecord{
		CanonicalURL: canonicalURL,
		JSONData:     raw,
	}, nil
}

func (s *embeddedDefinitionStore) ensureLoaded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.byURL = map[string][]byte{}
	rawResources, err := loadR4Bundle()
	if err == nil {
		for _, raw := range rawResources {
			var peek struct {
				ResourceType string `json:"resourceType"`
				URL          string `json:"url"`
			}
			if err := json.Unmarshal(raw, &peek); err != nil {
				continue
			}
			if peek.ResourceType != "StructureDefinition" || peek.URL == "" {
				continue
			}
			s.byURL[peek.URL] = raw
		}
	}
	s.loaded = true
}
