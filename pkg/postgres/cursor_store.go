package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CursorStore persists named sync cursors in Postgres.
type CursorStore struct {
	exec     execer
	tenantID string
}

func newCursorStore(pool *pgxpool.Pool, tenantID string) *CursorStore {
	return &CursorStore{exec: pool, tenantID: tenantID}
}

func newCursorStoreTx(tx execer, tenantID string) *CursorStore {
	return &CursorStore{exec: tx, tenantID: tenantID}
}

func (s *CursorStore) GetCursor(ctx context.Context, name string) (*store.Cursor, error) {
	var (
		position  string
		updatedAt time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT position, updated_at FROM hai_sync_cursor
		WHERE tenant_id = $1 AND name = $2`, s.tenantID, name,
	).Scan(&position, &updatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cursor: %w", err)
	}
	return &store.Cursor{
		Name:      name,
		Position:  position,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *CursorStore) UpsertCursor(ctx context.Context, cursor store.Cursor) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_sync_cursor (tenant_id, name, position, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
			position = EXCLUDED.position,
			updated_at = EXCLUDED.updated_at`,
		s.tenantID, cursor.Name, cursor.Position, cursor.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert cursor: %w", err)
	}
	return nil
}

func (s *CursorStore) DeleteCursor(ctx context.Context, name string) error {
	_, err := s.exec.Exec(ctx, `
		DELETE FROM hai_sync_cursor WHERE tenant_id = $1 AND name = $2`, s.tenantID, name)
	if err != nil {
		return fmt.Errorf("delete cursor: %w", err)
	}
	return nil
}
