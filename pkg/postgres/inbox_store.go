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

func newInboxStoreTx(tx pgx.Tx, tenantID string) *InboxStore {
	return &InboxStore{exec: tx, tenantID: tenantID}
}

// ClaimPush atomically claims a previously unseen push event ID for this
// transaction. PostgreSQL waits for a concurrent claimant to commit or roll
// back before resolving the conflict, so only one transaction performs the
// resource/conflict side effects for a first delivery.
func (s *InboxStore) ClaimPush(ctx context.Context, id string, claimedAt time.Time) (bool, error) {
	var claimed bool
	err := s.exec.QueryRow(ctx, `
		INSERT INTO hai_sync_inbox_applied (tenant_id, id, applied_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, id) DO NOTHING
		RETURNING true`,
		s.tenantID, id, claimedAt,
	).Scan(&claimed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim push inbox event: %w", err)
	}
	return claimed, nil
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

// MarkAppliedWithPayload records a hub acknowledgement together with the
// idempotency marker. Existing acknowledgement payloads are preserved on an
// idempotent retry.
func (s *InboxStore) MarkAppliedWithPayload(ctx context.Context, id string, payload []byte, appliedAt time.Time) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_sync_inbox_applied (tenant_id, id, applied_at, ack_payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, id) DO UPDATE SET applied_at = EXCLUDED.applied_at,
			ack_payload = COALESCE(hai_sync_inbox_applied.ack_payload, EXCLUDED.ack_payload)`,
		s.tenantID, id, appliedAt, payload,
	)
	if err != nil {
		return fmt.Errorf("mark inbox applied with payload: %w", err)
	}
	return nil
}

// GetAckPayload returns the stored push acknowledgement, if the inbox row
// exists. Pull inbox rows have no acknowledgement payload.
func (s *InboxStore) GetAckPayload(ctx context.Context, id string) ([]byte, bool, error) {
	var payload []byte
	err := s.exec.QueryRow(ctx, `
		SELECT ack_payload FROM hai_sync_inbox_applied
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id).Scan(&payload)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read inbox acknowledgement: %w", err)
	}
	return payload, true, nil
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
