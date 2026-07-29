package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// DefinitionStore persists FHIR definition resources in SQLite.
type DefinitionStore struct {
	db *sql.DB
}

func newDefinitionStore(db *sql.DB) *DefinitionStore {
	return &DefinitionStore{db: db}
}

func (s *DefinitionStore) Upsert(ctx context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin definition upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := upsertDefinition(ctx, tx, record, targets); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit definition upsert: %w", err)
	}
	return nil
}

func upsertDefinition(ctx context.Context, exec queryExec, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO hai_definition_resource (
			canonical_url, version, fhir_version, fhir_resource_type, definition_kind,
			name, status, package_name, package_version, module_name, json_data, installed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (canonical_url, version) DO UPDATE SET
			fhir_version = excluded.fhir_version,
			fhir_resource_type = excluded.fhir_resource_type,
			definition_kind = excluded.definition_kind,
			name = excluded.name,
			status = excluded.status,
			package_name = excluded.package_name,
			package_version = excluded.package_version,
			module_name = excluded.module_name,
			json_data = excluded.json_data,
			installed_at = excluded.installed_at`,
		record.CanonicalURL, record.Version, record.FHIRVersion, record.FHIRResourceType,
		string(record.DefinitionKind), record.Name, record.Status,
		record.PackageName, record.PackageVersion, record.ModuleName,
		record.JSONData, formatTime(record.InstalledAt),
	)
	if err != nil {
		return fmt.Errorf("upsert definition resource: %w", err)
	}

	if _, err := exec.ExecContext(ctx, `
		DELETE FROM hai_definition_target WHERE canonical_url = ? AND version = ?`,
		record.CanonicalURL, record.Version,
	); err != nil {
		return fmt.Errorf("clear definition targets: %w", err)
	}

	for _, target := range targets {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO hai_definition_target (canonical_url, version, target_resource_type, target_role)
			VALUES (?, ?, ?, ?)`,
			target.CanonicalURL, target.Version, target.TargetResourceType, target.TargetRole,
		); err != nil {
			return fmt.Errorf("insert definition target: %w", err)
		}
	}
	return nil
}

func (s *DefinitionStore) Get(ctx context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	var record store.DefinitionResourceRecord
	var kind string
	var installed string
	err := s.db.QueryRowContext(ctx, `
		SELECT canonical_url, version, fhir_version, fhir_resource_type, definition_kind,
			name, status, package_name, package_version, module_name, json_data, installed_at
		FROM hai_definition_resource
		WHERE canonical_url = ? AND version = ?`, canonicalURL, version,
	).Scan(
		&record.CanonicalURL, &record.Version, &record.FHIRVersion, &record.FHIRResourceType,
		&kind, &record.Name, &record.Status, &record.PackageName, &record.PackageVersion,
		&record.ModuleName, &record.JSONData, &installed,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("definition not found: %s|%s", canonicalURL, version)
	}
	if err != nil {
		return nil, fmt.Errorf("get definition: %w", err)
	}
	record.DefinitionKind = store.DefinitionKind(kind)
	ts, err := parseTime(installed)
	if err != nil {
		return nil, err
	}
	record.InstalledAt = ts
	return &record, nil
}

func (s *DefinitionStore) Delete(ctx context.Context, canonicalURL, version string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin definition delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM hai_definition_target WHERE canonical_url = ? AND version = ?`,
		canonicalURL, version,
	); err != nil {
		return fmt.Errorf("delete definition targets: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM hai_definition_resource WHERE canonical_url = ? AND version = ?`,
		canonicalURL, version,
	)
	if err != nil {
		return fmt.Errorf("delete definition resource: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit definition delete: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("definition not found: %s|%s", canonicalURL, version)
	}
	return nil
}

func (s *DefinitionStore) List(ctx context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	query := `
		SELECT DISTINCT dr.canonical_url, dr.version, dr.fhir_version, dr.fhir_resource_type,
			dr.definition_kind, dr.name, dr.status, dr.package_name, dr.package_version,
			dr.module_name, dr.json_data, dr.installed_at
		FROM hai_definition_resource dr`
	var joins []string
	var where []string
	var args []any

	if filter.TargetResourceType != "" {
		joins = append(joins, `
			INNER JOIN hai_definition_target dt
				ON dt.canonical_url = dr.canonical_url AND dt.version = dr.version`)
		where = append(where, "dt.target_resource_type = ?")
		args = append(args, filter.TargetResourceType)
	}
	if filter.FHIRVersion != "" {
		where = append(where, "dr.fhir_version = ?")
		args = append(args, filter.FHIRVersion)
	}
	if filter.DefinitionKind != "" {
		where = append(where, "dr.definition_kind = ?")
		args = append(args, string(filter.DefinitionKind))
	}
	if filter.CanonicalURL != "" {
		where = append(where, "dr.canonical_url = ?")
		args = append(args, filter.CanonicalURL)
	}
	if filter.PackageName != "" {
		where = append(where, "dr.package_name = ?")
		args = append(args, filter.PackageName)
	}
	if filter.ModuleName != "" {
		where = append(where, "dr.module_name = ?")
		args = append(args, filter.ModuleName)
	}

	query += strings.Join(joins, "")
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY dr.canonical_url ASC, dr.version ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list definitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.DefinitionResourceRecord
	for rows.Next() {
		var record store.DefinitionResourceRecord
		var kind string
		var installed string
		if err := rows.Scan(
			&record.CanonicalURL, &record.Version, &record.FHIRVersion, &record.FHIRResourceType,
			&kind, &record.Name, &record.Status, &record.PackageName, &record.PackageVersion,
			&record.ModuleName, &record.JSONData, &installed,
		); err != nil {
			return nil, fmt.Errorf("scan definition row: %w", err)
		}
		record.DefinitionKind = store.DefinitionKind(kind)
		ts, err := parseTime(installed)
		if err != nil {
			return nil, err
		}
		record.InstalledAt = ts
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate definitions: %w", err)
	}
	return out, nil
}
