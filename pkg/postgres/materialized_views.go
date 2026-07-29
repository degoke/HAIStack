package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaterializedViewStore persists named materialized projections in Postgres.
type MaterializedViewStore struct {
	exec     querier
	tenantID string
}

func newMaterializedViewStore(pool *pgxpool.Pool, tenantID string) *MaterializedViewStore {
	return &MaterializedViewStore{exec: pool, tenantID: tenantID}
}

func (s *MaterializedViewStore) Upsert(ctx context.Context, record store.MaterializedViewRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_materialized_view (tenant_id, view_name, key, payload, version, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, view_name, key) DO UPDATE SET
			payload = EXCLUDED.payload,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at`,
		s.tenantID, record.ViewName, record.Key, record.Payload, record.Version, record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert materialized view: %w", err)
	}
	return nil
}

func (s *MaterializedViewStore) Get(ctx context.Context, viewName, key string) (*store.MaterializedViewRecord, error) {
	var record store.MaterializedViewRecord
	err := s.exec.QueryRow(ctx, `
		SELECT view_name, key, payload, version, updated_at
		FROM hai_materialized_view
		WHERE tenant_id = $1 AND view_name = $2 AND key = $3`,
		s.tenantID, viewName, key,
	).Scan(&record.ViewName, &record.Key, &record.Payload, &record.Version, &record.UpdatedAt)
	if isNoRows(err) {
		return nil, fmt.Errorf("materialized view not found: %s/%s", viewName, key)
	}
	if err != nil {
		return nil, fmt.Errorf("get materialized view: %w", err)
	}
	return &record, nil
}

func (s *MaterializedViewStore) Delete(ctx context.Context, viewName, key string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_materialized_view
		WHERE tenant_id = $1 AND view_name = $2 AND key = $3`,
		s.tenantID, viewName, key,
	)
	if err != nil {
		return fmt.Errorf("delete materialized view: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("materialized view not found: %s/%s", viewName, key)
	}
	return nil
}

func (s *MaterializedViewStore) ListKeys(ctx context.Context, viewName string) ([]string, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT key FROM hai_materialized_view
		WHERE tenant_id = $1 AND view_name = $2
		ORDER BY key ASC`, s.tenantID, viewName)
	if err != nil {
		return nil, fmt.Errorf("list materialized view keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan materialized view key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate materialized view keys: %w", err)
	}
	return keys, nil
}
