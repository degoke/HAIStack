package store

import (
	"context"
	"strings"
	"time"
	"unicode"
)

// DefinitionKind identifies a catalog entry kind for registry storage and compilation.
type DefinitionKind string

const (
	DefinitionKindStructureDefinition DefinitionKind = "structure-definition"
	DefinitionKindSearchParameter     DefinitionKind = "search-parameter"
	DefinitionKindCodeSystem          DefinitionKind = "code-system"
	DefinitionKindValueSet            DefinitionKind = "value-set"
	DefinitionKindConceptMap          DefinitionKind = "concept-map"
)

// DefinitionKindFromResourceType maps a FHIR definition resource type to a catalog kind slug.
// Known MVP kinds are normalized; future kinds use a kebab-case slug derived from the type name.
func DefinitionKindFromResourceType(resourceType string) DefinitionKind {
	switch resourceType {
	case "StructureDefinition":
		return DefinitionKindStructureDefinition
	case "SearchParameter":
		return DefinitionKindSearchParameter
	case "CodeSystem":
		return DefinitionKindCodeSystem
	case "ValueSet":
		return DefinitionKindValueSet
	case "ConceptMap":
		return DefinitionKindConceptMap
	default:
		return DefinitionKind(camelToKebab(resourceType))
	}
}

func camelToKebab(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// DefinitionResourceRecord stores one raw FHIR definition resource as JSON.
type DefinitionResourceRecord struct {
	CanonicalURL     string         `json:"canonicalUrl"`
	Version          string         `json:"version"`
	FHIRVersion      string         `json:"fhirVersion"`
	FHIRResourceType string         `json:"fhirResourceType"`
	DefinitionKind   DefinitionKind `json:"definitionKind"`
	Name             string         `json:"name"`
	Status           string         `json:"status"`
	PackageName      string         `json:"packageName,omitempty"`
	PackageVersion   string         `json:"packageVersion,omitempty"`
	ModuleName       string         `json:"moduleName,omitempty"`
	JSONData         []byte         `json:"jsonData"`
	InstalledAt      time.Time      `json:"installedAt"`
}

// DefinitionTargetRecord indexes a definition against one target resource type.
type DefinitionTargetRecord struct {
	CanonicalURL       string `json:"canonicalUrl"`
	Version            string `json:"version"`
	TargetResourceType string `json:"targetResourceType"`
	TargetRole         string `json:"targetRole"`
}

// RegistryInstallRecord tracks tenant/runtime enablement and installed definition references.
type RegistryInstallRecord struct {
	DefinitionKind     DefinitionKind `json:"definitionKind"`
	CanonicalURL       string         `json:"canonicalUrl"`
	Version            string         `json:"version"`
	TargetResourceType string         `json:"targetResourceType"`
	Enabled            bool           `json:"enabled"`
	SourceModule       string         `json:"sourceModule,omitempty"`
	InstalledAt        time.Time      `json:"installedAt"`
}

// DefinitionFilter selects definition resources from the catalog.
type DefinitionFilter struct {
	FHIRVersion        string
	DefinitionKind     DefinitionKind
	CanonicalURL       string
	PackageName        string
	ModuleName         string
	TargetResourceType string
}

// RegistryInstallFilter selects install/enablement rows.
type RegistryInstallFilter struct {
	TargetResourceType string
	DefinitionKind     DefinitionKind
	CanonicalURL       string
	Version            string
}

// DefinitionStore persists raw FHIR definition resources and their target mappings.
type DefinitionStore interface {
	Upsert(ctx context.Context, record DefinitionResourceRecord, targets []DefinitionTargetRecord) error
	Get(ctx context.Context, canonicalURL, version string) (*DefinitionResourceRecord, error)
	List(ctx context.Context, filter DefinitionFilter) ([]DefinitionResourceRecord, error)
	Delete(ctx context.Context, canonicalURL, version string) error
}

// RegistryInstallStore persists resource enablement and installed definition references.
type RegistryInstallStore interface {
	SetEnabled(ctx context.Context, record RegistryInstallRecord) error
	UpsertInstall(ctx context.Context, record RegistryInstallRecord) error
	ListEnabled(ctx context.Context) ([]RegistryInstallRecord, error)
	ListInstalled(ctx context.Context, filter RegistryInstallFilter) ([]RegistryInstallRecord, error)
	Delete(ctx context.Context, filter RegistryInstallFilter) error
}
