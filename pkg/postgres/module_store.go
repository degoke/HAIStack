package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModuleStore persists module registration metadata in Postgres.
type ModuleStore struct {
	exec     querier
	tenantID string
}

func newModuleStore(pool *pgxpool.Pool, tenantID string) *ModuleStore {
	return &ModuleStore{exec: pool, tenantID: tenantID}
}

func (s *ModuleStore) Register(ctx context.Context, module store.ModuleRecord) error {
	metadata, err := json.Marshal(module.Metadata)
	if err != nil {
		return fmt.Errorf("marshal module metadata: %w", err)
	}
	_, err = s.exec.Exec(ctx, `
		INSERT INTO hai_module_registry (tenant_id, name, version, metadata, registered_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
			version = EXCLUDED.version,
			metadata = EXCLUDED.metadata,
			registered_at = EXCLUDED.registered_at`,
		s.tenantID, module.Name, module.Version, metadata, module.RegisteredAt,
	)
	if err != nil {
		return fmt.Errorf("register module: %w", err)
	}
	return nil
}

func (s *ModuleStore) Get(ctx context.Context, name string) (*store.ModuleRecord, error) {
	var (
		record     store.ModuleRecord
		metadata   []byte
		registered time.Time
	)
	err := s.exec.QueryRow(ctx, `
		SELECT name, version, metadata, registered_at
		FROM hai_module_registry
		WHERE tenant_id = $1 AND name = $2`, s.tenantID, name,
	).Scan(&record.Name, &record.Version, &metadata, &registered)
	if isNoRows(err) {
		return nil, fmt.Errorf("module not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get module: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &record.Metadata)
	}
	record.RegisteredAt = registered
	return &record, nil
}

func (s *ModuleStore) List(ctx context.Context) ([]store.ModuleRecord, error) {
	rows, err := s.exec.Query(ctx, `
		SELECT name, version, metadata, registered_at
		FROM hai_module_registry
		WHERE tenant_id = $1
		ORDER BY name ASC`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}
	defer rows.Close()

	var out []store.ModuleRecord
	for rows.Next() {
		var (
			record     store.ModuleRecord
			metadata   []byte
			registered time.Time
		)
		if err := rows.Scan(&record.Name, &record.Version, &metadata, &registered); err != nil {
			return nil, fmt.Errorf("scan module row: %w", err)
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &record.Metadata)
		}
		record.RegisteredAt = registered
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate modules: %w", err)
	}
	return out, nil
}

func (s *ModuleStore) Unregister(ctx context.Context, name string) error {
	tag, err := s.exec.Exec(ctx, `
		DELETE FROM hai_module_registry WHERE tenant_id = $1 AND name = $2`, s.tenantID, name)
	if err != nil {
		return fmt.Errorf("unregister module: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("module not found: %s", name)
	}
	return nil
}
