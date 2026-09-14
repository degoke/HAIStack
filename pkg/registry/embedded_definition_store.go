package registry

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// EmbeddedDefinitionStore resolves HL7 R4 base StructureDefinitions from the embedded bundle.
func EmbeddedDefinitionStore() store.DefinitionStore {
	return embeddedDefinitionStoreInst
}

var embeddedDefinitionStoreInst = &embeddedDefinitionStore{}

type embeddedDefinitionStore struct {
	mu     sync.Mutex
	byURL  map[string][]byte
	loaded map[string]bool
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
	raw, ok := s.load(canonicalURL)
	if !ok || len(raw) == 0 {
		return nil, context.Canceled
	}
	return &store.DefinitionResourceRecord{
		CanonicalURL: canonicalURL,
		JSONData:     raw,
	}, nil
}

func (s *embeddedDefinitionStore) load(canonicalURL string) ([]byte, bool) {
	canonicalURL = structureDefinitionURLWithoutVersion(canonicalURL)
	s.mu.Lock()
	if s.byURL == nil {
		s.byURL = map[string][]byte{}
		s.loaded = map[string]bool{}
	}
	if raw, ok := s.byURL[canonicalURL]; ok {
		s.mu.Unlock()
		return raw, true
	}
	if s.loaded[canonicalURL] {
		s.mu.Unlock()
		return nil, false
	}
	s.mu.Unlock()

	raw, ok := readEmbeddedStructureDefinition(canonicalURL)

	s.mu.Lock()
	s.loaded[canonicalURL] = true
	if ok {
		s.byURL[canonicalURL] = raw
	}
	s.mu.Unlock()
	return raw, ok
}

func readEmbeddedStructureDefinition(canonicalURL string) ([]byte, bool) {
	path, ok := embeddedStructureDefinitionPath(canonicalURL)
	if !ok {
		return nil, false
	}
	raw, err := fs.ReadFile(r4BundleFS, path)
	if err != nil {
		return nil, false
	}
	var peek struct {
		ResourceType string `json:"resourceType"`
		URL          string `json:"url"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return nil, false
	}
	if peek.ResourceType != "StructureDefinition" || peek.URL != canonicalURL {
		return nil, false
	}
	return raw, true
}

func structureDefinitionURLWithoutVersion(canonicalURL string) string {
	if idx := strings.Index(canonicalURL, "|"); idx >= 0 {
		return canonicalURL[:idx]
	}
	return canonicalURL
}

func embeddedStructureDefinitionPath(canonicalURL string) (string, bool) {
	canonicalURL = structureDefinitionURLWithoutVersion(canonicalURL)
	const prefix = "http://hl7.org/fhir/StructureDefinition/"
	if !strings.HasPrefix(canonicalURL, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(canonicalURL, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return "internal/bundles/r4/structure-definitions/" + id + ".json", true
}
