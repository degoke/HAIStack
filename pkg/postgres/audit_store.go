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
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_audit_log (
			id, tenant_id, timestamp, actor, action, resource_type, resource_id, outcome, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		record.ID,
		s.tenantID,
		record.Timestamp,
		record.Actor,
		record.Action,
		nullString(record.ResourceType),
		nullString(record.ResourceID),
		nullString(record.Outcome),
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
	args = append(args, s.tenantID)
	argN++

	if query.ResourceType != "" {
		clauses = append(clauses, fmt.Sprintf("resource_type = $%d", argN))
		args = append(args, query.ResourceType)
		argN++
	}
	if query.ResourceID != "" {
		clauses = append(clauses, fmt.Sprintf("resource_id = $%d", argN))
		args = append(args, query.ResourceID)
		argN++
	}
	if query.Actor != "" {
		clauses = append(clauses, fmt.Sprintf("actor = $%d", argN))
		args = append(args, query.Actor)
		argN++
	}
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
		SELECT id, timestamp, actor, action, resource_type, resource_id, outcome, details
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
			record       store.AuditRecord
			resourceType *string
			resourceID   *string
			outcome      *string
			detailsJSON  []byte
		)
		if err := rows.Scan(
			&record.ID, &record.Timestamp, &record.Actor, &record.Action,
			&resourceType, &resourceID, &outcome, &detailsJSON,
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
