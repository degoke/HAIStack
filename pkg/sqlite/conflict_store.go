package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// ConflictStore persists sync and write conflicts in SQLite.
//
// Append, List, and Resolve are supported. Conflict detection and reconciliation
// policy live outside this package.
type ConflictStore struct {
	exec conflictExec
}

type conflictExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func newConflictStore(db *sql.DB) *ConflictStore {
	return &ConflictStore{exec: db}
}

//nolint:unused // used by transactional write sessions
func newConflictStoreTx(tx *sql.Tx) *ConflictStore {
	return &ConflictStore{exec: tx}
}

func (s *ConflictStore) Append(ctx context.Context, record store.ConflictRecord) error {
	var resolvedAt any
	if record.ResolvedAt != nil {
		resolvedAt = formatTime(*record.ResolvedAt)
	}
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_sync_conflict (
			id, resource_type, resource_id,
			local_version_id, remote_version_id, reason, created_at, resolved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		record.ResourceType,
		record.ResourceID,
		record.LocalVersionID,
		record.RemoteVersionID,
		record.Reason,
		formatTime(record.CreatedAt),
		resolvedAt,
	)
	if err != nil {
		return fmt.Errorf("append conflict: %w", err)
	}
	return nil
}

func (s *ConflictStore) List(ctx context.Context, resourceType, resourceID string) ([]store.ConflictRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT id, resource_type, resource_id, local_version_id, remote_version_id, reason, created_at, resolved_at
		FROM hai_sync_conflict
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY created_at ASC`,
		resourceType, resourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.ConflictRecord
	for rows.Next() {
		var (
			record     store.ConflictRecord
			createdAt  string
			resolvedAt sql.NullString
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
		ts, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		record.CreatedAt = ts
		if resolvedAt.Valid {
			resolved, err := parseTime(resolvedAt.String)
			if err != nil {
				return nil, err
			}
			record.ResolvedAt = &resolved
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conflicts: %w", err)
	}
	return out, nil
}

func (s *ConflictStore) Resolve(ctx context.Context, id string, resolvedAt time.Time) error {
	result, err := s.exec.ExecContext(ctx, `
		UPDATE hai_sync_conflict SET resolved_at = ? WHERE id = ?`,
		formatTime(resolvedAt), id,
	)
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve conflict rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("conflict not found: %s", id)
	}
	return nil
}
