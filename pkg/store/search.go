package store

import "context"

// SearchIndexEntry holds indexed metadata or prepared search tokens for one resource.
// Backends may persist extracted fields, token sets, or other index-oriented data.
// This package does not parse FHIR search expressions.
type SearchIndexEntry struct {
	ResourceType string            `json:"resourceType"`
	ID           string            `json:"id"`
	Fields       map[string]string `json:"fields,omitempty"`
}

// PreparedQuery identifies a backend-specific prepared lookup or query plan.
type PreparedQuery struct {
	Name string `json:"name"`
}

// SearchMatch identifies one typed predicate scoped to a resource type.
type SearchMatch struct {
	ResourceType string
	FieldKey     string
	Value        string
	Operator     string
}

// ReferenceLink is one indexed reference from a source resource.
type ReferenceLink struct {
	TargetType string
	TargetID   string
	Literal    string
}

// FullTextMatch holds ranked full-text search matches.
type FullTextMatch struct {
	IDs    []string
	Scores map[string]float64
}

// SearchAdvancedExecutor extends typed lookups with advanced FHIR search operations.
// Postgres implements this interface; SQLite supports basic LookupMatch only.
type SearchAdvancedExecutor interface {
	SearchQueryExecutor
	LookupReferences(ctx context.Context, resourceType, fieldKey string, sourceIDs []string) (map[string][]ReferenceLink, error)
	LookupReferencing(ctx context.Context, sourceType, fieldKey, targetType, targetID string) ([]string, error)
	LookupFullText(ctx context.Context, resourceType, query string) (FullTextMatch, error)
}

// SearchIndexMaintainer supports reindex orphan cleanup.
type SearchIndexMaintainer interface {
	ListIndexedResourceIDs(ctx context.Context, resourceType string) ([]string, error)
}

// SearchQueryExecutor executes typed index lookups for FHIR search.
// Postgres implements this interface; SQLite supports index writes and simple lookups only.
type SearchQueryExecutor interface {
	LookupMatch(ctx context.Context, match SearchMatch) ([]string, error)
	FieldValues(ctx context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error)
}

// SearchStore persists search-index records and returns candidate resource IDs.
// It is index-oriented only; callers prepare lookup keys or queries outside this package.
type SearchStore interface {
	Index(ctx context.Context, entry SearchIndexEntry) error
	RemoveIndex(ctx context.Context, resourceType, id string) error
	Lookup(ctx context.Context, key, value string) ([]string, error)
	QueryPrepared(ctx context.Context, query PreparedQuery, args map[string]string) ([]string, error)
}
