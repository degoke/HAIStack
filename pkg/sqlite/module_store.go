package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// ModuleStore persists local module registration metadata in SQLite.
//
// Name, version, and metadata only; module code loading and activation policy live elsewhere.
type ModuleStore struct {
	exec moduleExec
}

type moduleExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func newModuleStore(db *sql.DB) *ModuleStore {
	return &ModuleStore{exec: db}
}

func (s *ModuleStore) Register(ctx context.Context, module store.ModuleRecord) error {
	metadata, err := json.Marshal(module.Metadata)
	if err != nil {
		return fmt.Errorf("marshal module metadata: %w", err)
	}
	_, err = s.exec.ExecContext(ctx, `
		INSERT INTO hai_module_registry (name, version, metadata, registered_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			version = excluded.version,
			metadata = excluded.metadata,
			registered_at = excluded.registered_at`,
		module.Name, module.Version, metadata, formatTime(module.RegisteredAt),
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
		registered string
	)
	err := s.exec.QueryRowContext(ctx, `
		SELECT name, version, metadata, registered_at
		FROM hai_module_registry WHERE name = ?`, name,
	).Scan(&record.Name, &record.Version, &metadata, &registered)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("module not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get module: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &record.Metadata)
	}
	ts, err := parseTime(registered)
	if err != nil {
		return nil, err
	}
	record.RegisteredAt = ts
	return &record, nil
}

func (s *ModuleStore) List(ctx context.Context) ([]store.ModuleRecord, error) {
	rows, err := s.exec.QueryContext(ctx, `
		SELECT name, version, metadata, registered_at
		FROM hai_module_registry
		ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.ModuleRecord
	for rows.Next() {
		var (
			record     store.ModuleRecord
			metadata   []byte
			registered string
		)
		if err := rows.Scan(&record.Name, &record.Version, &metadata, &registered); err != nil {
			return nil, fmt.Errorf("scan module row: %w", err)
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &record.Metadata)
		}
		ts, err := parseTime(registered)
		if err != nil {
			return nil, err
		}
		record.RegisteredAt = ts
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate modules: %w", err)
	}
	return out, nil
}

func (s *ModuleStore) Unregister(ctx context.Context, name string) error {
	result, err := s.exec.ExecContext(ctx, `DELETE FROM hai_module_registry WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("unregister module: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("unregister module rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("module not found: %s", name)
	}
	return nil
}
