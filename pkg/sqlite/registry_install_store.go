package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// RegistryInstallStore persists local registry enablement and install references in SQLite.
type RegistryInstallStore struct {
	exec registryInstallExec
}

type registryInstallExec interface {
	queryExec
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func newRegistryInstallStore(db *sql.DB) *RegistryInstallStore {
	return &RegistryInstallStore{exec: db}
}

func (s *RegistryInstallStore) SetEnabled(ctx context.Context, record store.RegistryInstallRecord) error {
	enabled := 0
	if record.Enabled {
		enabled = 1
	}
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_registry_install (
			definition_kind, canonical_url, version, target_resource_type,
			enabled, source_module, installed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (definition_kind, canonical_url, version, target_resource_type) DO UPDATE SET
			enabled = excluded.enabled,
			source_module = excluded.source_module,
			installed_at = excluded.installed_at`,
		string(record.DefinitionKind), record.CanonicalURL, record.Version, record.TargetResourceType,
		enabled, record.SourceModule, formatTime(record.InstalledAt),
	)
	if err != nil {
		return fmt.Errorf("set registry enabled: %w", err)
	}
	return nil
}

func (s *RegistryInstallStore) UpsertInstall(ctx context.Context, record store.RegistryInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *RegistryInstallStore) ListEnabled(ctx context.Context) ([]store.RegistryInstallRecord, error) {
	rows, err := s.queryInstallRows(ctx, store.RegistryInstallFilter{})
	if err != nil {
		return nil, err
	}
	var out []store.RegistryInstallRecord
	for _, record := range rows {
		if record.Enabled {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *RegistryInstallStore) ListInstalled(ctx context.Context, filter store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	return s.queryInstallRows(ctx, filter)
}

func (s *RegistryInstallStore) Delete(ctx context.Context, filter store.RegistryInstallFilter) error {
	query := `DELETE FROM hai_registry_install`
	var where []string
	var args []any

	if filter.TargetResourceType != "" {
		where = append(where, "target_resource_type = ?")
		args = append(args, filter.TargetResourceType)
	}
	if filter.DefinitionKind != "" {
		where = append(where, "definition_kind = ?")
		args = append(args, string(filter.DefinitionKind))
	}
	if filter.CanonicalURL != "" {
		where = append(where, "canonical_url = ?")
		args = append(args, filter.CanonicalURL)
	}
	if filter.Version != "" {
		where = append(where, "version = ?")
		args = append(args, filter.Version)
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	if _, err := s.exec.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete registry installs: %w", err)
	}
	return nil
}

func (s *RegistryInstallStore) queryInstallRows(ctx context.Context, filter store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	query := `
		SELECT definition_kind, canonical_url, version, target_resource_type,
			enabled, source_module, installed_at
		FROM hai_registry_install`
	var where []string
	var args []any

	if filter.TargetResourceType != "" {
		where = append(where, "target_resource_type = ?")
		args = append(args, filter.TargetResourceType)
	}
	if filter.DefinitionKind != "" {
		where = append(where, "definition_kind = ?")
		args = append(args, string(filter.DefinitionKind))
	}

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY target_resource_type ASC, canonical_url ASC"

	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list registry installs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.RegistryInstallRecord
	for rows.Next() {
		record, err := scanRegistryInstallRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry installs: %w", err)
	}
	return out, nil
}

func scanRegistryInstallRow(rows *sql.Rows) (store.RegistryInstallRecord, error) {
	var record store.RegistryInstallRecord
	var kind string
	var enabled int
	var installed string
	if err := rows.Scan(
		&kind, &record.CanonicalURL, &record.Version, &record.TargetResourceType,
		&enabled, &record.SourceModule, &installed,
	); err != nil {
		return store.RegistryInstallRecord{}, fmt.Errorf("scan registry install row: %w", err)
	}
	record.DefinitionKind = store.DefinitionKind(kind)
	record.Enabled = enabled != 0
	ts, err := parseTime(installed)
	if err != nil {
		return store.RegistryInstallRecord{}, err
	}
	record.InstalledAt = ts
	return record, nil
}
