package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConflictStore records rejected and conflicted writes in Postgres.
type ConflictStore struct {
	exec     querier
	tenantID string
}

func newConflictStore(pool *pgxpool.Pool, tenantID string) *ConflictStore {
	return &ConflictStore{exec: pool, tenantID: tenantID}
}

func newConflictStoreTx(tx pgx.Tx, tenantID string) *ConflictStore {
	return &ConflictStore{exec: tx, tenantID: tenantID}
}

func (s *ConflictStore) Append(ctx context.Context, record store.ConflictRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_sync_conflict (
			id, tenant_id, resource_type, resource_id,
			local_version_id, remote_version_id, reason, created_at, resolved_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		record.ID,
		s.tenantID,
		record.ResourceType,
		record.ResourceID,
		record.LocalVersionID,
		record.RemoteVersionID,
		record.Reason,
		record.CreatedAt,
		record.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("append conflict: %w", err)
	}
	return nil
}

func (s *ConflictStore) List(ctx context.Context, resourceType, resourceID string) ([]store.ConflictRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT id, resource_type, resource_id, local_version_id, remote_version_id, reason, created_at, resolved_at
		FROM hai_sync_conflict
		WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3
		ORDER BY created_at ASC`,
		s.tenantID, resourceType, resourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	defer rows.Close()

	var out []store.ConflictRecord
	for rows.Next() {
		var (
			record     store.ConflictRecord
			createdAt  time.Time
			resolvedAt *time.Time
		)
		if err := rows.Scan(
			&record.ID,
			&record.ResourceType,
			&record.ResourceID,
			&record.LocalVersionID,
			&record.RemoteVersionID,
			&record.Reason,
			&createdAt,
			&resolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan conflict row: %w", err)
		}
		record.CreatedAt = createdAt
		record.ResolvedAt = resolvedAt
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conflicts: %w", err)
	}
	return out, nil
}

func (s *ConflictStore) Resolve(ctx context.Context, id string, resolvedAt time.Time) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE hai_sync_conflict SET resolved_at = $1
		WHERE tenant_id = $2 AND id = $3`,
		resolvedAt, s.tenantID, id,
	)
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("conflict not found: %s", id)
	}
	return nil
}
