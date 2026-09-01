package validate

import (
	"sync"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

// BaseStructureDefinitionURL is the canonical HL7 R4 base profile URL for a resource type.
func BaseStructureDefinitionURL(resourceType string) string {
	return "http://hl7.org/fhir/StructureDefinition/" + resourceType
}

// RegistryProfileCatalog resolves StructureDefinitions from a compiled registry snapshot.
type RegistryProfileCatalog struct {
	snapshot *registry.Snapshot
	cache    map[string]*StructureDefinition
	mu       sync.RWMutex
}

// NewRegistryProfileCatalog builds a ProfileCatalog backed by registry.Snapshot.
func NewRegistryProfileCatalog(snapshot *registry.Snapshot) *RegistryProfileCatalog {
	return &RegistryProfileCatalog{
		snapshot: snapshot,
		cache:    make(map[string]*StructureDefinition),
	}
}

// GetStructureDefinition implements ProfileCatalog.
func (c *RegistryProfileCatalog) GetStructureDefinition(canonicalURL string) (*StructureDefinition, bool) {
	if c == nil || c.snapshot == nil || canonicalURL == "" {
		return nil, false
	}
	c.mu.RLock()
	if sd, ok := c.cache[canonicalURL]; ok {
		c.mu.RUnlock()
		return sd, true
	}
	c.mu.RUnlock()

	raw, ok := c.lookupRaw(canonicalURL)
	if !ok {
		return nil, false
	}
	sd, ok, err := parseStructureDefinition(raw)
	if err != nil || !ok {
		return nil, false
	}
	c.mu.Lock()
	c.cache[canonicalURL] = sd
	c.mu.Unlock()
	return sd, true
}

func (c *RegistryProfileCatalog) lookupRaw(canonicalURL string) ([]byte, bool) {
	if data, ok := c.snapshot.DefinitionsByCanonical(canonicalURL, "4.0.1"); ok {
		return data, true
	}
	if data, ok := c.snapshot.AnyDefinitionByCanonical(canonicalURL); ok {
		return data, true
	}
	const prefix = "http://hl7.org/fhir/StructureDefinition/"
	if len(canonicalURL) > len(prefix) && canonicalURL[:len(prefix)] == prefix {
		resourceType := canonicalURL[len(prefix):]
		return c.snapshot.StructureDefinitionFor(resourceType)
	}
	return nil, false
}
