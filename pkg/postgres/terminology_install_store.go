package postgres

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TerminologyInstallStore persists tenant-scoped terminology pack enablement.
type TerminologyInstallStore struct {
	exec     querier
	tenantID string
}

func newTerminologyInstallStore(pool *pgxpool.Pool, tenantID string) *TerminologyInstallStore {
	return &TerminologyInstallStore{exec: pool, tenantID: tenantID}
}

func (s *TerminologyInstallStore) SetEnabled(ctx context.Context, record store.TerminologyInstallRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_terminology_install (
			tenant_id, pack_name, pack_version, resource_type, canonical_url, version,
			enabled, source_module, installed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, resource_type, canonical_url, version) DO UPDATE SET
			pack_name = EXCLUDED.pack_name,
			pack_version = EXCLUDED.pack_version,
			enabled = EXCLUDED.enabled,
			source_module = EXCLUDED.source_module,
			installed_at = EXCLUDED.installed_at`,
		s.tenantID, record.PackName, record.PackVersion, record.ResourceType,
		record.CanonicalURL, record.Version, record.Enabled, record.SourceModule, record.InstalledAt,
	)
	if err != nil {
		return fmt.Errorf("set terminology install enabled: %w", err)
	}
	return nil
}

func (s *TerminologyInstallStore) UpsertInstall(ctx context.Context, record store.TerminologyInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *TerminologyInstallStore) ListEnabled(ctx context.Context) ([]store.TerminologyInstallRecord, error) {
	rows, err := s.queryInstallRows(ctx, store.TerminologyInstallFilter{})
	if err != nil {
		return nil, err
	}
	var out []store.TerminologyInstallRecord
	for _, record := range rows {
		if record.Enabled {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *TerminologyInstallStore) ListInstalled(ctx context.Context, filter store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	return s.queryInstallRows(ctx, filter)
}

func (s *TerminologyInstallStore) Delete(ctx context.Context, filter store.TerminologyInstallFilter) error {
	query := `DELETE FROM hai_terminology_install WHERE tenant_id = $1`
	args := []any{s.tenantID}
	argN := 2
	if filter.PackName != "" {
		query += fmt.Sprintf(" AND pack_name = $%d", argN)
		args = append(args, filter.PackName)
		argN++
	}
	if filter.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, filter.ResourceType)
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
		return fmt.Errorf("delete terminology installs: %w", err)
	}
	return nil
}

func (s *TerminologyInstallStore) queryInstallRows(ctx context.Context, filter store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	query := `
		SELECT pack_name, pack_version, resource_type, canonical_url, version,
			enabled, source_module, installed_at
		FROM hai_terminology_install
		WHERE tenant_id = $1`
	args := []any{s.tenantID}
	argN := 2
	if filter.PackName != "" {
		query += fmt.Sprintf(" AND pack_name = $%d", argN)
		args = append(args, filter.PackName)
		argN++
	}
	if filter.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, filter.ResourceType)
	}
	query += " ORDER BY canonical_url ASC, version ASC"

	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list terminology installs: %w", err)
	}
	defer rows.Close()

	var out []store.TerminologyInstallRecord
	for rows.Next() {
		var record store.TerminologyInstallRecord
		if err := rows.Scan(
			&record.PackName, &record.PackVersion, &record.ResourceType,
			&record.CanonicalURL, &record.Version, &record.Enabled,
			&record.SourceModule, &record.InstalledAt,
		); err != nil {
			return nil, fmt.Errorf("scan terminology install row: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminology installs: %w", err)
	}
	return out, nil
}
