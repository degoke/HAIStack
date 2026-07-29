package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnalyticsStore persists analytics events in Postgres.
type AnalyticsStore struct {
	exec     querier
	tenantID string
}

func newAnalyticsStore(pool *pgxpool.Pool, tenantID string) *AnalyticsStore {
	return &AnalyticsStore{exec: pool, tenantID: tenantID}
}

func (s *AnalyticsStore) Append(ctx context.Context, event store.AnalyticsEvent) error {
	dimensions, err := json.Marshal(event.Dimensions)
	if err != nil {
		return fmt.Errorf("marshal analytics dimensions: %w", err)
	}
	values, err := json.Marshal(event.Values)
	if err != nil {
		return fmt.Errorf("marshal analytics values: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_analytics_event (id, tenant_id, name, timestamp, dimensions, values, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, s.tenantID, event.Name, event.Timestamp, dimensions, values, event.Payload,
	)
	if err != nil {
		return fmt.Errorf("append analytics event: %w", err)
	}
	return nil
}

func (s *AnalyticsStore) QueryPrepared(ctx context.Context, query store.PreparedQuery, args map[string]string) ([]store.AnalyticsEvent, error) {
	if query.Name != "by-name-since" {
		return nil, nil
	}
	name := args["name"]
	since := args["since"]
	rows, err := s.exec.Query(ctx, `
		SELECT id, name, timestamp, dimensions, values, payload
		FROM hai_analytics_event
		WHERE tenant_id = $1 AND name = $2 AND timestamp >= $3::timestamptz
		ORDER BY timestamp ASC`,
		s.tenantID, name, since,
	)
	if err != nil {
		return nil, fmt.Errorf("query analytics: %w", err)
	}
	defer rows.Close()

	var out []store.AnalyticsEvent
	for rows.Next() {
		var (
			event          store.AnalyticsEvent
			dimensionsJSON []byte
			valuesJSON     []byte
		)
		if err := rows.Scan(&event.ID, &event.Name, &event.Timestamp, &dimensionsJSON, &valuesJSON, &event.Payload); err != nil {
			return nil, fmt.Errorf("scan analytics row: %w", err)
		}
		if len(dimensionsJSON) > 0 {
			_ = json.Unmarshal(dimensionsJSON, &event.Dimensions)
		}
		if len(valuesJSON) > 0 {
			_ = json.Unmarshal(valuesJSON, &event.Values)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analytics: %w", err)
	}
	return out, nil
}
