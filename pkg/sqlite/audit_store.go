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
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO hai_audit_log (
			id, timestamp, actor, action, resource_type, resource_id, outcome,
			tenant, subject, view_name, tool_name, conversation_id, module_name, blob_key, details
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		formatTime(record.Timestamp),
		record.Actor,
		record.Action,
		nullString(record.ResourceType),
		nullString(record.ResourceID),
		nullString(record.Outcome),
		nullString(record.Tenant),
		nullString(record.Subject),
		nullString(record.ViewName),
		nullString(record.ToolName),
		nullString(record.ConversationID),
		nullString(record.ModuleName),
		nullString(record.BlobKey),
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
	appendFilter := func(column string, value string) {
		if value == "" {
			return
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, value)
	}
	appendFilter("resource_type", query.ResourceType)
	appendFilter("resource_id", query.ResourceID)
	appendFilter("actor", query.Actor)
	appendFilter("action", query.Action)
	appendFilter("outcome", query.Outcome)
	appendFilter("tenant", query.Tenant)
	appendFilter("subject", query.Subject)
	appendFilter("view_name", query.ViewName)
	appendFilter("tool_name", query.ToolName)
	appendFilter("conversation_id", query.ConversationID)
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
		SELECT id, timestamp, actor, action, resource_type, resource_id, outcome,
			       tenant, subject, view_name, tool_name, conversation_id, module_name, blob_key, details
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
		record, err := scanAuditRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit: %w", err)
	}
	return out, nil
}

func scanAuditRecord(rows *sql.Rows) (store.AuditRecord, error) {
	var (
		record         store.AuditRecord
		timestamp      string
		resourceType   sql.NullString
		resourceID     sql.NullString
		outcome        sql.NullString
		tenant         sql.NullString
		subject        sql.NullString
		viewName       sql.NullString
		toolName       sql.NullString
		conversationID sql.NullString
		moduleName     sql.NullString
		blobKey        sql.NullString
		detailsJSON    sql.NullString
	)
	if err := rows.Scan(
		&record.ID, &timestamp, &record.Actor, &record.Action,
		&resourceType, &resourceID, &outcome,
		&tenant, &subject, &viewName, &toolName, &conversationID, &moduleName, &blobKey, &detailsJSON,
	); err != nil {
		return store.AuditRecord{}, fmt.Errorf("scan audit row: %w", err)
	}
	ts, err := parseTime(timestamp)
	if err != nil {
		return store.AuditRecord{}, fmt.Errorf("parse audit timestamp: %w", err)
	}
	record.Timestamp = ts
	record.ResourceType = nullStringValue(resourceType)
	record.ResourceID = nullStringValue(resourceID)
	record.Outcome = nullStringValue(outcome)
	record.Tenant = nullStringValue(tenant)
	record.Subject = nullStringValue(subject)
	record.ViewName = nullStringValue(viewName)
	record.ToolName = nullStringValue(toolName)
	record.ConversationID = nullStringValue(conversationID)
	record.ModuleName = nullStringValue(moduleName)
	record.BlobKey = nullStringValue(blobKey)
	if detailsJSON.Valid && detailsJSON.String != "" {
		_ = json.Unmarshal([]byte(detailsJSON.String), &record.Details)
	}
	return record, nil
}

func nullStringValue(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}
