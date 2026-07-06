package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// SearchStore persists typed search index rows in SQLite.
type SearchStore struct {
	exec searchExec
}

type searchExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type searchTable string

const (
	searchTableToken     searchTable = "search_token"
	searchTableString    searchTable = "search_string"
	searchTableDate      searchTable = "search_date"
	searchTableNumber    searchTable = "search_number"
	searchTableReference searchTable = "search_reference"
)

func newSearchStore(db *sql.DB) *SearchStore {
	return &SearchStore{exec: db}
}

func newSearchStoreTx(tx *sql.Tx) *SearchStore {
	return &SearchStore{exec: tx}
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
		if strings.HasPrefix(fieldKey, "composite.") || strings.HasPrefix(fieldKey, "text.") {
			return fmt.Errorf("sqlite search store does not support advanced index key %q", fieldKey)
		}
		table, normalizedKey, err := parseSearchFieldKey(fieldKey)
		if err != nil {
			return err
		}
		query := fmt.Sprintf(`
			INSERT OR IGNORE INTO %s (resource_type, resource_id, field_key, value)
			VALUES (?, ?, ?, ?)`, table)
		if _, err := s.exec.ExecContext(ctx, query, entry.ResourceType, entry.ID, normalizedKey, value); err != nil {
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
		query := fmt.Sprintf(`DELETE FROM %s WHERE resource_type = ? AND resource_id = ?`, table)
		if _, err := s.exec.ExecContext(ctx, query, resourceType, id); err != nil {
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
		WHERE field_key = ? AND value = ?
		ORDER BY resource_id`, table)
	rows, err := s.exec.QueryContext(ctx, query, fieldKey, value)
	if err != nil {
		return nil, fmt.Errorf("lookup search index: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	return nil, fmt.Errorf("unknown prepared search query %q", query.Name)
}

// LookupMatch returns resource IDs matching one typed predicate within a resource type.
func (s *SearchStore) LookupMatch(ctx context.Context, match store.SearchMatch) ([]string, error) {
	table, fieldKey, err := parseSearchFieldKey(match.FieldKey)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT resource_id FROM %s
		WHERE resource_type = ? AND field_key = ? AND value = ?
		ORDER BY resource_id`, table)
	rows, err := s.exec.QueryContext(ctx, query, match.ResourceType, fieldKey, match.Value)
	if err != nil {
		return nil, fmt.Errorf("lookup search match: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	placeholders := make([]string, len(resourceIDs))
	args := make([]any, 0, 2+len(resourceIDs))
	args = append(args, resourceType, normalizedKey)
	for i, id := range resourceIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(`
		SELECT resource_id, value FROM %s
		WHERE resource_type = ? AND field_key = ? AND resource_id IN (%s)`,
		table, strings.Join(placeholders, ","))
	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("field values lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
