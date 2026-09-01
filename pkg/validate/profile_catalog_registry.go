package validate

import (
	"errors"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

// ErrProfileNotFound indicates a StructureDefinition URL is not in the catalog.
var ErrProfileNotFound = errors.New("profile not found")

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
	sd, err := c.ResolveStructureDefinition(canonicalURL)
	if err != nil {
		return nil, false
	}
	return sd, true
}

// ResolveStructureDefinition returns a parsed StructureDefinition or a lookup/parse error.
func (c *RegistryProfileCatalog) ResolveStructureDefinition(canonicalURL string) (*StructureDefinition, error) {
	if c == nil || c.snapshot == nil || canonicalURL == "" {
		return nil, ErrProfileNotFound
	}
	c.mu.RLock()
	if sd, ok := c.cache[canonicalURL]; ok {
		c.mu.RUnlock()
		return sd, nil
	}
	c.mu.RUnlock()

	raw, ok := c.lookupRaw(canonicalURL)
	if !ok {
		return nil, ErrProfileNotFound
	}
	sd, ok, err := parseStructureDefinition(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrProfileNotFound
	}
	c.mu.Lock()
	c.cache[canonicalURL] = sd
	c.mu.Unlock()
	return sd, nil
}

type structureDefinitionResolver interface {
	ResolveStructureDefinition(canonicalURL string) (*StructureDefinition, error)
}

func lookupStructureDefinition(catalog ProfileCatalog, canonicalURL string) (*StructureDefinition, error) {
	if catalog == nil {
		return nil, ErrProfileNotFound
	}
	if resolver, ok := catalog.(structureDefinitionResolver); ok {
		return resolver.ResolveStructureDefinition(canonicalURL)
	}
	sd, ok := catalog.GetStructureDefinition(canonicalURL)
	if !ok {
		return nil, ErrProfileNotFound
	}
	return sd, nil
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
