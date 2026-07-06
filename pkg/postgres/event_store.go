package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventStore persists append-only accepted-write events in Postgres.
type EventStore struct {
	exec     querier
	tenantID string
}

func newEventStore(pool *pgxpool.Pool, tenantID string) *EventStore {
	return &EventStore{exec: pool, tenantID: tenantID}
}

func newEventStoreTx(tx pgx.Tx, tenantID string) *EventStore {
	return &EventStore{exec: tx, tenantID: tenantID}
}

func (s *EventStore) Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	var sequence int64
	err := s.exec.QueryRow(ctx, `
		INSERT INTO event_log (tenant_id, resource_type, resource_id, version_id, action, timestamp, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING sequence`,
		s.tenantID,
		event.ResourceType,
		event.ID,
		event.VersionID,
		string(event.Action),
		event.Timestamp,
		nullString(event.Hash),
	).Scan(&sequence)
	if err != nil {
		return store.ResourceEvent{}, fmt.Errorf("append event: %w", err)
	}
	event.Sequence = sequence
	return event, nil
}

func (s *EventStore) ReadSince(ctx context.Context, afterSequence int64, limit int) ([]store.ResourceEvent, error) {
	query := `
		SELECT sequence, resource_type, resource_id, version_id, action, timestamp, hash
		FROM event_log
		WHERE tenant_id = $1 AND sequence > $2
		ORDER BY sequence ASC`
	args := []any{s.tenantID, afterSequence}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read events since %d: %w", afterSequence, err)
	}
	defer rows.Close()

	var out []store.ResourceEvent
	for rows.Next() {
		var (
			event     store.ResourceEvent
			timestamp time.Time
			hash      *string
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
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		event.Timestamp = timestamp
		if hash != nil {
			event.Hash = *hash
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return out, nil
}

func (s *EventStore) LatestForResource(ctx context.Context, resourceType, id string) (*store.ResourceEvent, error) {
	var (
		event     store.ResourceEvent
		timestamp time.Time
		hash      *string
	)
	err := s.exec.QueryRow(ctx, `
		SELECT sequence, resource_type, resource_id, version_id, action, timestamp, hash
		FROM event_log
		WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3
		ORDER BY sequence DESC
		LIMIT 1`, s.tenantID, resourceType, id,
	).Scan(
		&event.Sequence,
		&event.ResourceType,
		&event.ID,
		&event.VersionID,
		&event.Action,
		&timestamp,
		&hash,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest event for %s/%s: %w", resourceType, id, err)
	}
	event.Timestamp = timestamp
	if hash != nil {
		event.Hash = *hash
	}
	return &event, nil
}
