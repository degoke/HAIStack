package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// CursorStore persists named sync cursors in SQLite.
type CursorStore struct {
	exec queryExec
}

func newCursorStore(db *sql.DB) *CursorStore {
	return &CursorStore{exec: db}
}

func newCursorStoreTx(tx *sql.Tx) *CursorStore {
	return &CursorStore{exec: tx}
}

func (s *CursorStore) GetCursor(ctx context.Context, name string) (*store.Cursor, error) {
	var (
		position  string
		updatedAt string
	)
	err := s.exec.QueryRowContext(ctx, `
		SELECT position, updated_at FROM hai_sync_cursor WHERE name = ?`, name,
	).Scan(&position, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cursor: %w", err)
	}
	ts, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	return &store.Cursor{
		Name:      name,
		Position:  position,
		UpdatedAt: ts,
	}, nil
}

func (s *CursorStore) UpsertCursor(ctx context.Context, cursor store.Cursor) error {
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_sync_cursor (name, position, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			position = excluded.position,
			updated_at = excluded.updated_at`,
		cursor.Name, cursor.Position, formatTime(cursor.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert cursor: %w", err)
	}
	return nil
}

func (s *CursorStore) DeleteCursor(ctx context.Context, name string) error {
	_, err := s.exec.ExecContext(ctx, `DELETE FROM hai_sync_cursor WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete cursor: %w", err)
	}
	return nil
}
