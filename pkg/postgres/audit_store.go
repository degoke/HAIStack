package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditStore persists append-only audit records in Postgres.
type AuditStore struct {
	exec     querier
	tenantID string
}

func newAuditStore(pool *pgxpool.Pool, tenantID string) *AuditStore {
	return &AuditStore{exec: pool, tenantID: tenantID}
}

func newAuditStoreTx(tx querier, tenantID string) *AuditStore {
	return &AuditStore{exec: tx, tenantID: tenantID}
}

func (s *AuditStore) Append(ctx context.Context, record store.AuditRecord) error {
	details, err := json.Marshal(record.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	tenant := s.tenantID
	if record.Tenant != "" {
		tenant = record.Tenant
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_audit_log (
			id, tenant_id, timestamp, actor, action, resource_type, resource_id, outcome,
			subject, view_name, tool_name, conversation_id, module_name, blob_key, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		record.ID,
		tenant,
		record.Timestamp,
		record.Actor,
		record.Action,
		nullString(record.ResourceType),
		nullString(record.ResourceID),
		nullString(record.Outcome),
		nullString(record.Subject),
		nullString(record.ViewName),
		nullString(record.ToolName),
		nullString(record.ConversationID),
		nullString(record.ModuleName),
		nullString(record.BlobKey),
		details,
	)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

func (s *AuditStore) List(ctx context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	var (
		clauses []string
		args    []any
		argN    = 1
	)

	clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", argN))
	tenant := s.tenantID
	if query.Tenant != "" {
		tenant = query.Tenant
	}
	args = append(args, tenant)
	argN++

	appendFilter := func(column, value string) {
		if value == "" {
			return
		}
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, argN))
		args = append(args, value)
		argN++
	}
	appendFilter("resource_type", query.ResourceType)
	appendFilter("resource_id", query.ResourceID)
	appendFilter("actor", query.Actor)
	appendFilter("action", query.Action)
	appendFilter("outcome", query.Outcome)
	appendFilter("subject", query.Subject)
	appendFilter("view_name", query.ViewName)
	appendFilter("tool_name", query.ToolName)
	appendFilter("conversation_id", query.ConversationID)
	if !query.After.IsZero() {
		clauses = append(clauses, fmt.Sprintf("timestamp >= $%d", argN))
		args = append(args, query.After)
		argN++
	}
	if !query.Before.IsZero() {
		clauses = append(clauses, fmt.Sprintf("timestamp <= $%d", argN))
		args = append(args, query.Before)
	}

	sql := fmt.Sprintf(`
		SELECT id, timestamp, actor, action, resource_type, resource_id, outcome,
		       tenant_id, subject, view_name, tool_name, conversation_id, module_name, blob_key, details
		FROM hai_audit_log
		WHERE %s
		ORDER BY timestamp ASC`, strings.Join(clauses, " AND "))
	if query.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", query.Limit)
	}

	rows, err := s.exec.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []store.AuditRecord
	for rows.Next() {
		var (
			record         store.AuditRecord
			resourceType   *string
			resourceID     *string
			outcome        *string
			tenantID       string
			subject        *string
			viewName       *string
			toolName       *string
			conversationID *string
			moduleName     *string
			blobKey        *string
			detailsJSON    []byte
		)
		if err := rows.Scan(
			&record.ID, &record.Timestamp, &record.Actor, &record.Action,
			&resourceType, &resourceID, &outcome,
			&tenantID, &subject, &viewName, &toolName, &conversationID, &moduleName, &blobKey, &detailsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		if resourceType != nil {
			record.ResourceType = *resourceType
		}
		if resourceID != nil {
			record.ResourceID = *resourceID
		}
		if outcome != nil {
			record.Outcome = *outcome
		}
		record.Tenant = tenantID
		if subject != nil {
			record.Subject = *subject
		}
		if viewName != nil {
			record.ViewName = *viewName
		}
		if toolName != nil {
			record.ToolName = *toolName
		}
		if conversationID != nil {
			record.ConversationID = *conversationID
		}
		if moduleName != nil {
			record.ModuleName = *moduleName
		}
		if blobKey != nil {
			record.BlobKey = *blobKey
		}
		if len(detailsJSON) > 0 {
			_ = json.Unmarshal(detailsJSON, &record.Details)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit: %w", err)
	}
	return out, nil
}
