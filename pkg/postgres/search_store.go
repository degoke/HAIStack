package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchStore persists typed search index rows in Postgres.
type SearchStore struct {
	exec     querier
	tenantID string
}

type searchTable string

const (
	searchTableToken     searchTable = "search_token"
	searchTableString    searchTable = "search_string"
	searchTableDate      searchTable = "search_date"
	searchTableNumber    searchTable = "search_number"
	searchTableReference searchTable = "search_reference"
)

func newSearchStore(pool *pgxpool.Pool, tenantID string) *SearchStore {
	return &SearchStore{exec: pool, tenantID: tenantID}
}

func newSearchStoreTx(tx pgx.Tx, tenantID string) *SearchStore {
	return &SearchStore{exec: tx, tenantID: tenantID}
}

func parseSearchFieldKey(key string) (searchTable, string, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 1 {
		return searchTableString, key, nil
	}
	switch parts[0] {
	case "token":
		return searchTableToken, parts[1], nil
	case "string":
		return searchTableString, parts[1], nil
	case "date":
		return searchTableDate, parts[1], nil
	case "number":
		return searchTableNumber, parts[1], nil
	case "reference", "ref":
		return searchTableReference, parts[1], nil
	default:
		return searchTableString, key, nil
	}
}

func (s *SearchStore) Index(ctx context.Context, entry store.SearchIndexEntry) error {
	for fieldKey, value := range entry.Fields {
		table, normalizedKey, err := parseSearchFieldKey(fieldKey)
		if err != nil {
			return err
		}
		query := fmt.Sprintf(`
			INSERT INTO %s (tenant_id, resource_type, resource_id, field_key, value)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, table)
		if _, err := s.exec.Exec(ctx, query, s.tenantID, entry.ResourceType, entry.ID, normalizedKey, value); err != nil {
			return fmt.Errorf("index search field %s: %w", fieldKey, err)
		}
	}
	return nil
}

func (s *SearchStore) RemoveIndex(ctx context.Context, resourceType, id string) error {
	tables := []searchTable{
		searchTableToken,
		searchTableString,
		searchTableDate,
		searchTableNumber,
		searchTableReference,
	}
	for _, table := range tables {
		query := fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3`, table)
		if _, err := s.exec.Exec(ctx, query, s.tenantID, resourceType, id); err != nil {
			return fmt.Errorf("remove search index from %s: %w", table, err)
		}
	}
	return nil
}

func (s *SearchStore) Lookup(ctx context.Context, key, value string) ([]string, error) {
	table, fieldKey, err := parseSearchFieldKey(key)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT resource_id FROM %s
		WHERE tenant_id = $1 AND field_key = $2 AND value = $3
		ORDER BY resource_id`, table)
	rows, err := s.exec.Query(ctx, query, s.tenantID, fieldKey, value)
	if err != nil {
		return nil, fmt.Errorf("lookup search index: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan search lookup: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search lookup: %w", err)
	}
	return ids, nil
}

func (s *SearchStore) QueryPrepared(ctx context.Context, query store.PreparedQuery, args map[string]string) ([]string, error) {
	if query.Name == "by-field" {
		return s.Lookup(ctx, args["key"], args["value"])
	}
	return nil, nil
}

// LookupMatch returns resource IDs matching one typed predicate within a resource type.
func (s *SearchStore) LookupMatch(ctx context.Context, match store.SearchMatch) ([]string, error) {
	table, fieldKey, err := parseSearchFieldKey(match.FieldKey)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT resource_id FROM %s
		WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value = $4
		ORDER BY resource_id`, table)
	rows, err := s.exec.Query(ctx, query, s.tenantID, match.ResourceType, fieldKey, match.Value)
	if err != nil {
		return nil, fmt.Errorf("lookup search match: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan search match: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search match: %w", err)
	}
	return ids, nil
}

// FieldValues returns one indexed value per resource ID for a typed field key.
func (s *SearchStore) FieldValues(ctx context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error) {
	if len(resourceIDs) == 0 {
		return map[string]string{}, nil
	}
	table, normalizedKey, err := parseSearchFieldKey(fieldKey)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT resource_id, value FROM %s
		WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND resource_id = ANY($4)`, table)
	rows, err := s.exec.Query(ctx, query, s.tenantID, resourceType, normalizedKey, resourceIDs)
	if err != nil {
		return nil, fmt.Errorf("field values lookup: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(resourceIDs))
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			return nil, fmt.Errorf("scan field value: %w", err)
		}
		if _, ok := out[id]; !ok {
			out[id] = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate field values: %w", err)
	}
	return out, nil
}
