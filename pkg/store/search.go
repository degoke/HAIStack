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

// SearchMatch identifies one typed equality predicate scoped to a resource type.
type SearchMatch struct {
	ResourceType string
	FieldKey     string
	Value        string
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
