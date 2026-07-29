package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.InboxStore = (*InboxStore)(nil)

// InboxStore tracks applied remote sync operations for idempotency.
//
// MarkApplied, IsApplied, and AppliedAt implement store.InboxStore.
type InboxStore struct {
	exec queryExec
}

func newInboxStore(db *sql.DB) *InboxStore {
	return &InboxStore{exec: db}
}

// MarkApplied records that a remote operation has been applied locally.
func (s *InboxStore) MarkApplied(ctx context.Context, id string, appliedAt time.Time) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT OR REPLACE INTO hai_sync_inbox_applied (id, applied_at)
		VALUES (?, ?)`,
		id, formatTime(appliedAt),
	)
	if err != nil {
		return fmt.Errorf("mark inbox applied: %w", err)
	}
	return nil
}

// IsApplied reports whether a remote operation has already been applied.
func (s *InboxStore) IsApplied(ctx context.Context, id string) (bool, error) {
	var count int
	err := s.exec.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM hai_sync_inbox_applied WHERE id = ?`, id,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check inbox applied: %w", err)
	}
	return count > 0, nil
}

// AppliedAt returns when a remote operation was applied, if recorded.
func (s *InboxStore) AppliedAt(ctx context.Context, id string) (*time.Time, error) {
	var appliedAt string
	err := s.exec.QueryRowContext(ctx, `
		SELECT applied_at FROM hai_sync_inbox_applied WHERE id = ?`, id,
	).Scan(&appliedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read inbox applied at: %w", err)
	}
	ts, err := parseTime(appliedAt)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}
