package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegistryInstallStore persists tenant-scoped registry enablement and install references.
type RegistryInstallStore struct {
	exec     querier
	tenantID string
}

func newRegistryInstallStore(pool *pgxpool.Pool, tenantID string) *RegistryInstallStore {
	return &RegistryInstallStore{exec: pool, tenantID: tenantID}
}

func (s *RegistryInstallStore) SetEnabled(ctx context.Context, record store.RegistryInstallRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_registry_install (
			tenant_id, definition_kind, canonical_url, version, target_resource_type,
			enabled, source_module, installed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, definition_kind, canonical_url, version, target_resource_type) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			source_module = EXCLUDED.source_module,
			installed_at = EXCLUDED.installed_at`,
		s.tenantID, string(record.DefinitionKind), record.CanonicalURL, record.Version,
		record.TargetResourceType, record.Enabled, record.SourceModule, record.InstalledAt,
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
	query := `DELETE FROM hai_registry_install WHERE tenant_id = $1`
	args := []any{s.tenantID}
	argN := 2

	if filter.TargetResourceType != "" {
		query += fmt.Sprintf(" AND target_resource_type = $%d", argN)
		args = append(args, filter.TargetResourceType)
		argN++
	}
	if filter.DefinitionKind != "" {
		query += fmt.Sprintf(" AND definition_kind = $%d", argN)
		args = append(args, string(filter.DefinitionKind))
		argN++
	}
	if filter.CanonicalURL != "" {
		query += fmt.Sprintf(" AND canonical_url = $%d", argN)
		args = append(args, filter.CanonicalURL)
		argN++
	}
	if filter.Version != "" {
		query += fmt.Sprintf(" AND version = $%d", argN)
		args = append(args, filter.Version)
	}
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete registry installs: %w", err)
	}
	return nil
}

func (s *RegistryInstallStore) queryInstallRows(ctx context.Context, filter store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	query := `
		SELECT definition_kind, canonical_url, version, target_resource_type,
			enabled, source_module, installed_at
		FROM hai_registry_install
		WHERE tenant_id = $1`
	args := []any{s.tenantID}
	argN := 2

	if filter.TargetResourceType != "" {
		query += fmt.Sprintf(" AND target_resource_type = $%d", argN)
		args = append(args, filter.TargetResourceType)
		argN++
	}
	if filter.DefinitionKind != "" {
		query += fmt.Sprintf(" AND definition_kind = $%d", argN)
		args = append(args, string(filter.DefinitionKind))
	}
	query += " ORDER BY target_resource_type ASC, canonical_url ASC"

	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list registry installs: %w", err)
	}
	defer rows.Close()

	var out []store.RegistryInstallRecord
	for rows.Next() {
		var record store.RegistryInstallRecord
		var kind string
		if err := rows.Scan(
			&kind, &record.CanonicalURL, &record.Version, &record.TargetResourceType,
			&record.Enabled, &record.SourceModule, &record.InstalledAt,
		); err != nil {
			return nil, fmt.Errorf("scan registry install row: %w", err)
		}
		record.DefinitionKind = store.DefinitionKind(kind)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry installs: %w", err)
	}
	return out, nil
}
