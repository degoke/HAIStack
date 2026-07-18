package store

import "context"

// TerminologyResourceRecord is the canonical source used to rebuild indexes.
type TerminologyResourceRecord struct {
	ScopeID, ResourceType, ResourceID, CanonicalURL, Version, Status string
	ResourceJSON                                                     []byte
	ContentMode, SourceModule                                        string
}

type TerminologyConceptRecord struct {
	ScopeID, SystemURL, SystemVersion, Code, Display, Definition string
	Active, Abstract                                             bool
	ParentCode, PropertiesJSON, DesignationsJSON                 string
}

type TerminologyValueSetRecord struct {
	ScopeID, CanonicalURL, Version, Status, ComposeJSON string
	ExpansionJSON                                       []byte
	ExpansionTimestamp                                  string
	ExpansionFingerprint                                string
}

type TerminologyExpansionMemberRecord struct {
	ScopeID, ValueSetURL, ValueSetVersion, SystemURL, SystemVersion, Code, Display string
	Inactive                                                                       bool
}

// TerminologyStore persists canonical terminology metadata and disposable projections.
type TerminologyStore interface {
	FindResource(ctx context.Context, scopeID, resourceType, canonicalURL, version string) (*TerminologyResourceRecord, error)
	PutResource(ctx context.Context, record TerminologyResourceRecord) error
	DeleteResource(ctx context.Context, scopeID, resourceType, canonicalURL, version string) error
	ListResources(ctx context.Context, scopeID, resourceType string) ([]TerminologyResourceRecord, error)
	ReplaceCodeSystem(ctx context.Context, scopeID, systemURL, version string, concepts []TerminologyConceptRecord) error
	LookupConcept(ctx context.Context, scopeID, systemURL, version, code string) (*TerminologyConceptRecord, error)
	ReplaceValueSet(ctx context.Context, record TerminologyValueSetRecord, members []TerminologyExpansionMemberRecord) error
	GetValueSet(ctx context.Context, scopeID, url, version string) (*TerminologyValueSetRecord, error)
	ListValueSetMembers(ctx context.Context, scopeID, url, version string) ([]TerminologyExpansionMemberRecord, error)
	DeleteProjections(ctx context.Context, scopeID, resourceType, canonicalURL, version string) error
}
