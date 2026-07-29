package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// AuditStore persists append-only audit records in SQLite.
type AuditStore struct {
	db *sql.DB
}

func newAuditStore(db *sql.DB) *AuditStore {
	return &AuditStore{db: db}
}

var _ store.AuditStore = (*AuditStore)(nil)

// Append implements store.AuditStore.
func (s *AuditStore) Append(ctx context.Context, record store.AuditRecord) error {
	details, err := json.Marshal(record.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	var resourceType, resourceID, outcome any
	if record.ResourceType != "" {
		resourceType = record.ResourceType
	}
	if record.ResourceID != "" {
		resourceID = record.ResourceID
	}
	if record.Outcome != "" {
		outcome = record.Outcome
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO hai_audit_log (
			id, timestamp, actor, action, resource_type, resource_id, outcome, details
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		formatTime(record.Timestamp),
		record.Actor,
		record.Action,
		resourceType,
		resourceID,
		outcome,
		string(details),
	)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// List implements store.AuditStore.
func (s *AuditStore) List(ctx context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	var (
		clauses []string
		args    []any
	)
	if query.ResourceType != "" {
		clauses = append(clauses, "resource_type = ?")
		args = append(args, query.ResourceType)
	}
	if query.ResourceID != "" {
		clauses = append(clauses, "resource_id = ?")
		args = append(args, query.ResourceID)
	}
	if query.Actor != "" {
		clauses = append(clauses, "actor = ?")
		args = append(args, query.Actor)
	}
	if !query.After.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, formatTime(query.After))
	}
	if !query.Before.IsZero() {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, formatTime(query.Before))
	}

	where := "1=1"
	if len(clauses) > 0 {
		where = strings.Join(clauses, " AND ")
	}
	sqlQuery := fmt.Sprintf(`
		SELECT id, timestamp, actor, action, resource_type, resource_id, outcome, details
		FROM hai_audit_log
		WHERE %s
		ORDER BY timestamp ASC`, where)
	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.AuditRecord
	for rows.Next() {
		var (
			record       store.AuditRecord
			timestamp    string
			resourceType sql.NullString
			resourceID   sql.NullString
			outcome      sql.NullString
			detailsJSON  sql.NullString
		)
		if err := rows.Scan(
			&record.ID, &timestamp, &record.Actor, &record.Action,
			&resourceType, &resourceID, &outcome, &detailsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		ts, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse audit timestamp: %w", err)
		}
		record.Timestamp = ts
		if resourceType.Valid {
			record.ResourceType = resourceType.String
		}
		if resourceID.Valid {
			record.ResourceID = resourceID.String
		}
		if outcome.Valid {
			record.Outcome = outcome.String
		}
		if detailsJSON.Valid && detailsJSON.String != "" {
			_ = json.Unmarshal([]byte(detailsJSON.String), &record.Details)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit: %w", err)
	}
	return out, nil
}
