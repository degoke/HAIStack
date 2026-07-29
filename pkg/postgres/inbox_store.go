package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ store.InboxStore = (*InboxStore)(nil)

// InboxStore tracks applied sync operations for idempotency.
type InboxStore struct {
	exec     querier
	tenantID string
}

func newInboxStore(pool *pgxpool.Pool, tenantID string) *InboxStore {
	return &InboxStore{exec: pool, tenantID: tenantID}
}

// MarkApplied records that a remote operation has been applied.
func (s *InboxStore) MarkApplied(ctx context.Context, id string, appliedAt time.Time) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_sync_inbox_applied (tenant_id, id, applied_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, id) DO UPDATE SET applied_at = EXCLUDED.applied_at`,
		s.tenantID, id, appliedAt,
	)
	if err != nil {
		return fmt.Errorf("mark inbox applied: %w", err)
	}
	return nil
}

// IsApplied reports whether a remote operation has already been applied.
func (s *InboxStore) IsApplied(ctx context.Context, id string) (bool, error) {
	var count int
	err := s.exec.QueryRow(ctx, `
		SELECT COUNT(1) FROM hai_sync_inbox_applied
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check inbox applied: %w", err)
	}
	return count > 0, nil
}

// AppliedAt returns when a remote operation was applied, if recorded.
func (s *InboxStore) AppliedAt(ctx context.Context, id string) (*time.Time, error) {
	var appliedAt time.Time
	err := s.exec.QueryRow(ctx, `
		SELECT applied_at FROM hai_sync_inbox_applied
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id,
	).Scan(&appliedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read inbox applied at: %w", err)
	}
	return &appliedAt, nil
}
