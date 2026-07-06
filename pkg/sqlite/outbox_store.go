package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// OutboxStore persists append-only sync outbox events in SQLite.
type OutboxStore struct {
	exec outboxExec
}

type outboxExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func newOutboxStore(db *sql.DB) *OutboxStore {
	return &OutboxStore{exec: db}
}

func newOutboxStoreTx(tx *sql.Tx) *OutboxStore {
	return &OutboxStore{exec: tx}
}

func (s *OutboxStore) Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	result, err := s.exec.ExecContext(ctx, `
		INSERT INTO sync_outbox (resource_type, resource_id, version_id, action, timestamp, hash)
		VALUES (?, ?, ?, ?, ?, ?)`,
		event.ResourceType,
		event.ID,
		event.VersionID,
		string(event.Action),
		formatTime(event.Timestamp),
		event.Hash,
	)
	if err != nil {
		return store.ResourceEvent{}, fmt.Errorf("append outbox event: %w", err)
	}

	sequence, err := result.LastInsertId()
	if err != nil {
		return store.ResourceEvent{}, fmt.Errorf("read outbox sequence: %w", err)
	}
	event.Sequence = sequence
	return event, nil
}

func (s *OutboxStore) ReadSince(ctx context.Context, afterSequence int64, limit int) ([]store.ResourceEvent, error) {
	query := `
		SELECT sequence, resource_type, resource_id, version_id, action, timestamp, hash
		FROM sync_outbox
		WHERE sequence > ?
		ORDER BY sequence ASC`
	args := []any{afterSequence}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read outbox since %d: %w", afterSequence, err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.ResourceEvent
	for rows.Next() {
		var (
			event     store.ResourceEvent
			timestamp string
			hash      sql.NullString
		)
		if err := rows.Scan(
			&event.Sequence,
			&event.ResourceType,
			&event.ID,
			&event.VersionID,
			&event.Action,
			&timestamp,
			&hash,
		); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		ts, err := parseTime(timestamp)
		if err != nil {
			return nil, err
		}
		event.Timestamp = ts
		if hash.Valid {
			event.Hash = hash.String
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}
	return out, nil
}

func (s *OutboxStore) LatestForResource(ctx context.Context, resourceType, id string) (*store.ResourceEvent, error) {
	var (
		event     store.ResourceEvent
		timestamp string
		hash      sql.NullString
	)
	err := s.exec.QueryRowContext(ctx, `
		SELECT sequence, resource_type, resource_id, version_id, action, timestamp, hash
		FROM sync_outbox
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY sequence DESC
		LIMIT 1`, resourceType, id,
	).Scan(
		&event.Sequence,
		&event.ResourceType,
		&event.ID,
		&event.VersionID,
		&event.Action,
		&timestamp,
		&hash,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest outbox event for %s/%s: %w", resourceType, id, err)
	}
	ts, err := parseTime(timestamp)
	if err != nil {
		return nil, err
	}
	event.Timestamp = ts
	if hash.Valid {
		event.Hash = hash.String
	}
	return &event, nil
}
