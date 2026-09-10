package sqlite

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// TerminologyInstallStore persists tenant-scoped terminology pack enablement.
type TerminologyInstallStore struct {
	exec     queryExec
	tenantID string
}

func newTerminologyInstallStore(e queryExec, tenantID string) *TerminologyInstallStore {
	if tenantID == "" {
		tenantID = "default"
	}
	return &TerminologyInstallStore{exec: e, tenantID: tenantID}
}

func (s *TerminologyInstallStore) SetEnabled(ctx context.Context, record store.TerminologyInstallRecord) error {
	enabled := 0
	if record.Enabled {
		enabled = 1
	}
	_, err := s.exec.ExecContext(ctx, `
		INSERT INTO hai_terminology_install (
			tenant_id, pack_name, pack_version, resource_type, canonical_url, version,
			enabled, source_module, installed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, resource_type, canonical_url, version) DO UPDATE SET
			pack_name = excluded.pack_name,
			pack_version = excluded.pack_version,
			enabled = excluded.enabled,
			source_module = excluded.source_module,
			installed_at = excluded.installed_at`,
		s.tenantID, record.PackName, record.PackVersion, record.ResourceType,
		record.CanonicalURL, record.Version, enabled, record.SourceModule, formatTime(record.InstalledAt),
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
	query := `DELETE FROM hai_terminology_install WHERE tenant_id = ?`
	args := []any{s.tenantID}
	if filter.PackName != "" {
		query += ` AND pack_name = ?`
		args = append(args, filter.PackName)
	}
	if filter.ResourceType != "" {
		query += ` AND resource_type = ?`
		args = append(args, filter.ResourceType)
	}
	if filter.CanonicalURL != "" {
		query += ` AND canonical_url = ?`
		args = append(args, filter.CanonicalURL)
	}
	if filter.Version != "" {
		query += ` AND version = ?`
		args = append(args, filter.Version)
	}
	if _, err := s.exec.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete terminology installs: %w", err)
	}
	return nil
}

func (s *TerminologyInstallStore) queryInstallRows(ctx context.Context, filter store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	query := `
		SELECT pack_name, pack_version, resource_type, canonical_url, version,
			enabled, source_module, installed_at
		FROM hai_terminology_install
		WHERE tenant_id = ?`
	args := []any{s.tenantID}
	if filter.PackName != "" {
		query += ` AND pack_name = ?`
		args = append(args, filter.PackName)
	}
	if filter.ResourceType != "" {
		query += ` AND resource_type = ?`
		args = append(args, filter.ResourceType)
	}
	query += ` ORDER BY canonical_url ASC, version ASC`

	rows, err := s.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list terminology installs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.TerminologyInstallRecord
	for rows.Next() {
		var record store.TerminologyInstallRecord
		var enabled int
		var installed string
		if err := rows.Scan(
			&record.PackName, &record.PackVersion, &record.ResourceType,
			&record.CanonicalURL, &record.Version, &enabled,
			&record.SourceModule, &installed,
		); err != nil {
			return nil, fmt.Errorf("scan terminology install row: %w", err)
		}
		record.Enabled = enabled != 0
		ts, err := parseTime(installed)
		if err != nil {
			return nil, err
		}
		record.InstalledAt = ts
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminology installs: %w", err)
	}
	return out, nil
}
