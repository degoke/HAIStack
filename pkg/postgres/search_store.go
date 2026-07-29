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
	searchTableToken     searchTable = "hai_search_token"
	searchTableString    searchTable = "hai_search_string"
	searchTableDate      searchTable = "hai_search_date"
	searchTableNumber    searchTable = "hai_search_number"
	searchTableReference searchTable = "hai_search_reference"
	searchTableComposite searchTable = "hai_search_composite"
	searchTableText      searchTable = "hai_search_text"
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
	case "composite":
		return searchTableComposite, parts[1], nil
	case "text":
		return searchTableText, parts[1], nil
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
		if table == searchTableText {
			query := `
				INSERT INTO hai_search_text (tenant_id, resource_type, resource_id, field_key, document)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (tenant_id, resource_type, resource_id, field_key)
				DO UPDATE SET document = EXCLUDED.document`
			if _, err := s.exec.Exec(ctx, query, s.tenantID, entry.ResourceType, entry.ID, normalizedKey, value); err != nil {
				return fmt.Errorf("index search text %s: %w", fieldKey, err)
			}
			continue
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
		searchTableComposite,
	}
	for _, table := range tables {
		query := fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3`, table)
		if _, err := s.exec.Exec(ctx, query, s.tenantID, resourceType, id); err != nil {
			return fmt.Errorf("remove search index from %s: %w", table, err)
		}
	}
	if _, err := s.exec.Exec(ctx, `DELETE FROM hai_search_text WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3`, s.tenantID, resourceType, id); err != nil {
		return fmt.Errorf("remove search text index: %w", err)
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
	return nil, fmt.Errorf("unknown prepared search query %q", query.Name)
}

// LookupMatch returns resource IDs matching one typed predicate within a resource type.
func (s *SearchStore) LookupMatch(ctx context.Context, match store.SearchMatch) ([]string, error) {
	table, fieldKey, err := parseSearchFieldKey(match.FieldKey)
	if err != nil {
		return nil, err
	}
	op := match.Operator
	if op == "" {
		op = "eq"
	}

	var query string
	var args []any
	switch {
	case table == searchTableString && op == "contains":
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value ILIKE '%%' || $4 || '%%'
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case table == searchTableString && op == "exact":
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value = $4
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case table == searchTableToken && op == "text":
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value ILIKE '%%' || $4 || '%%'
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case table == searchTableReference && op == "identifier":
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3
			  AND (value = $4 OR value LIKE '%%|' || $4 OR value LIKE '%%/' || $4)
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case table == searchTableReference && op == "type":
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3
			  AND (value LIKE $4 || '/%%' OR value LIKE $4 || '|%%')
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case (table == searchTableDate || table == searchTableNumber) && isComparator(op):
		sqlOp, err := comparatorSQL(op)
		if err != nil {
			return nil, err
		}
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value %s $4
			ORDER BY resource_id`, table, sqlOp)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	case op == "not":
		query = fmt.Sprintf(`
			SELECT DISTINCT r.resource_id FROM (
				SELECT resource_id FROM %s WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3
			) r
			WHERE r.resource_id NOT IN (
				SELECT resource_id FROM %s WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value = $4
			)
			ORDER BY resource_id`, table, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	default:
		query = fmt.Sprintf(`
			SELECT resource_id FROM %s
			WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value = $4
			ORDER BY resource_id`, table)
		args = []any{s.tenantID, match.ResourceType, fieldKey, match.Value}
	}

	rows, err := s.exec.Query(ctx, query, args...)
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

func isComparator(op string) bool {
	switch op {
	case "eq", "ne", "gt", "lt", "ge", "le", "sa", "eb", "ap":
		return true
	default:
		return false
	}
}

func comparatorSQL(op string) (string, error) {
	switch op {
	case "eq":
		return "=", nil
	case "ne":
		return "<>", nil
	case "gt":
		return ">", nil
	case "lt":
		return "<", nil
	case "ge":
		return ">=", nil
	case "le":
		return "<=", nil
	case "sa":
		return ">", nil
	case "eb":
		return "<", nil
	case "ap":
		return "=", nil
	default:
		return "", fmt.Errorf("unsupported comparator %q", op)
	}
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

// LookupReferences returns indexed reference links for source resources.
func (s *SearchStore) LookupReferences(ctx context.Context, resourceType, fieldKey string, sourceIDs []string) (map[string][]store.ReferenceLink, error) {
	if len(sourceIDs) == 0 {
		return map[string][]store.ReferenceLink{}, nil
	}
	_, normalizedKey, err := parseSearchFieldKey(fieldKey)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT resource_id, value FROM hai_search_reference
		WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND resource_id = ANY($4)
		ORDER BY resource_id, value`
	rows, err := s.exec.Query(ctx, query, s.tenantID, resourceType, normalizedKey, sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup references: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]store.ReferenceLink)
	for rows.Next() {
		var sourceID, value string
		if err := rows.Scan(&sourceID, &value); err != nil {
			return nil, fmt.Errorf("scan reference: %w", err)
		}
		link := parseReferenceValue(value)
		out[sourceID] = append(out[sourceID], link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate references: %w", err)
	}
	return out, nil
}

// LookupReferencing returns source resource IDs referencing a target resource.
func (s *SearchStore) LookupReferencing(ctx context.Context, sourceType, fieldKey, targetType, targetID string) ([]string, error) {
	_, normalizedKey, err := parseSearchFieldKey(fieldKey)
	if err != nil {
		return nil, err
	}
	values := []string{targetID, targetType + "/" + targetID, targetType + "|" + targetID}
	query := `
		SELECT DISTINCT resource_id FROM hai_search_reference
		WHERE tenant_id = $1 AND resource_type = $2 AND field_key = $3 AND value = ANY($4)
		ORDER BY resource_id`
	rows, err := s.exec.Query(ctx, query, s.tenantID, sourceType, normalizedKey, values)
	if err != nil {
		return nil, fmt.Errorf("lookup referencing: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan referencing: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate referencing: %w", err)
	}
	return ids, nil
}

// LookupFullText returns resource IDs ranked by Postgres full-text relevance.
func (s *SearchStore) LookupFullText(ctx context.Context, resourceType, queryText string) (store.FullTextMatch, error) {
	query := `
		SELECT resource_id, ts_rank(tsvector, plainto_tsquery('english', $3)) AS rank
		FROM hai_search_text
		WHERE tenant_id = $1 AND resource_type = $2
		  AND tsvector @@ plainto_tsquery('english', $3)
		ORDER BY rank DESC, resource_id`
	rows, err := s.exec.Query(ctx, query, s.tenantID, resourceType, queryText)
	if err != nil {
		return store.FullTextMatch{}, fmt.Errorf("lookup full text: %w", err)
	}
	defer rows.Close()

	out := store.FullTextMatch{Scores: make(map[string]float64)}
	for rows.Next() {
		var id string
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			return store.FullTextMatch{}, fmt.Errorf("scan full text: %w", err)
		}
		out.IDs = append(out.IDs, id)
		out.Scores[id] = rank
	}
	if err := rows.Err(); err != nil {
		return store.FullTextMatch{}, fmt.Errorf("iterate full text: %w", err)
	}
	return out, nil
}

// ListIndexedResourceIDs returns distinct resource IDs with any search index row.
func (s *SearchStore) ListIndexedResourceIDs(ctx context.Context, resourceType string) ([]string, error) {
	query := `
		SELECT DISTINCT resource_id FROM (
			SELECT resource_id FROM hai_search_token WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_string WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_date WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_number WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_reference WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_composite WHERE tenant_id = $1 AND resource_type = $2
			UNION SELECT resource_id FROM hai_search_text WHERE tenant_id = $1 AND resource_type = $2
		) ids
		ORDER BY resource_id`
	rows, err := s.exec.Query(ctx, query, s.tenantID, resourceType)
	if err != nil {
		return nil, fmt.Errorf("list indexed resource ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan indexed resource id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexed resource ids: %w", err)
	}
	return ids, nil
}

func parseReferenceValue(value string) store.ReferenceLink {
	if i := strings.Index(value, "|"); i >= 0 {
		parts := strings.SplitN(value, "|", 2)
		return store.ReferenceLink{
			TargetType: parts[0],
			TargetID:   parts[1],
			Literal:    value,
		}
	}
	if i := strings.Index(value, "/"); i >= 0 {
		return store.ReferenceLink{
			TargetType: value[:i],
			TargetID:   value[i+1:],
			Literal:    value,
		}
	}
	return store.ReferenceLink{TargetID: value, Literal: value}
}
