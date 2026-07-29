package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefinitionStore persists the global FHIR definition catalog in Postgres.
type DefinitionStore struct {
	pool *pgxpool.Pool
}

func newDefinitionStore(pool *pgxpool.Pool) *DefinitionStore {
	return &DefinitionStore{pool: pool}
}

func (s *DefinitionStore) Upsert(ctx context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin definition upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO hai_definition_resource (
			canonical_url, version, fhir_version, fhir_resource_type, definition_kind,
			name, status, package_name, package_version, module_name, json_data, installed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (canonical_url, version) DO UPDATE SET
			fhir_version = EXCLUDED.fhir_version,
			fhir_resource_type = EXCLUDED.fhir_resource_type,
			definition_kind = EXCLUDED.definition_kind,
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			package_name = EXCLUDED.package_name,
			package_version = EXCLUDED.package_version,
			module_name = EXCLUDED.module_name,
			json_data = EXCLUDED.json_data,
			installed_at = EXCLUDED.installed_at`,
		record.CanonicalURL, record.Version, record.FHIRVersion, record.FHIRResourceType,
		string(record.DefinitionKind), record.Name, record.Status,
		record.PackageName, record.PackageVersion, record.ModuleName,
		record.JSONData, record.InstalledAt,
	); err != nil {
		return fmt.Errorf("upsert definition resource: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM hai_definition_target WHERE canonical_url = $1 AND version = $2`,
		record.CanonicalURL, record.Version,
	); err != nil {
		return fmt.Errorf("clear definition targets: %w", err)
	}

	for _, target := range targets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO hai_definition_target (canonical_url, version, target_resource_type, target_role)
			VALUES ($1, $2, $3, $4)`,
			target.CanonicalURL, target.Version, target.TargetResourceType, target.TargetRole,
		); err != nil {
			return fmt.Errorf("insert definition target: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit definition upsert: %w", err)
	}
	return nil
}

func (s *DefinitionStore) Get(ctx context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	var record store.DefinitionResourceRecord
	var kind string
	err := s.pool.QueryRow(ctx, `
		SELECT canonical_url, version, fhir_version, fhir_resource_type, definition_kind,
			name, status, package_name, package_version, module_name, json_data, installed_at
		FROM hai_definition_resource
		WHERE canonical_url = $1 AND version = $2`, canonicalURL, version,
	).Scan(
		&record.CanonicalURL, &record.Version, &record.FHIRVersion, &record.FHIRResourceType,
		&kind, &record.Name, &record.Status, &record.PackageName, &record.PackageVersion,
		&record.ModuleName, &record.JSONData, &record.InstalledAt,
	)
	if isNoRows(err) {
		return nil, fmt.Errorf("definition not found: %s|%s", canonicalURL, version)
	}
	if err != nil {
		return nil, fmt.Errorf("get definition: %w", err)
	}
	record.DefinitionKind = store.DefinitionKind(kind)
	return &record, nil
}

func (s *DefinitionStore) Delete(ctx context.Context, canonicalURL, version string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin definition delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM hai_definition_target WHERE canonical_url = $1 AND version = $2`,
		canonicalURL, version,
	); err != nil {
		return fmt.Errorf("delete definition targets: %w", err)
	}
	result, err := tx.Exec(ctx, `
		DELETE FROM hai_definition_resource WHERE canonical_url = $1 AND version = $2`,
		canonicalURL, version,
	)
	if err != nil {
		return fmt.Errorf("delete definition resource: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit definition delete: %w", err)
	}
	if result.RowsAffected() == 0 {
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
	argN := 1

	if filter.TargetResourceType != "" {
		joins = append(joins, `
			INNER JOIN hai_definition_target dt
				ON dt.canonical_url = dr.canonical_url AND dt.version = dr.version`)
		where = append(where, fmt.Sprintf("dt.target_resource_type = $%d", argN))
		args = append(args, filter.TargetResourceType)
		argN++
	}
	if filter.FHIRVersion != "" {
		where = append(where, fmt.Sprintf("dr.fhir_version = $%d", argN))
		args = append(args, filter.FHIRVersion)
		argN++
	}
	if filter.DefinitionKind != "" {
		where = append(where, fmt.Sprintf("dr.definition_kind = $%d", argN))
		args = append(args, string(filter.DefinitionKind))
		argN++
	}
	if filter.CanonicalURL != "" {
		where = append(where, fmt.Sprintf("dr.canonical_url = $%d", argN))
		args = append(args, filter.CanonicalURL)
		argN++
	}
	if filter.PackageName != "" {
		where = append(where, fmt.Sprintf("dr.package_name = $%d", argN))
		args = append(args, filter.PackageName)
		argN++
	}
	if filter.ModuleName != "" {
		where = append(where, fmt.Sprintf("dr.module_name = $%d", argN))
		args = append(args, filter.ModuleName)
	}

	query += strings.Join(joins, "")
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY dr.canonical_url ASC, dr.version ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list definitions: %w", err)
	}
	defer rows.Close()

	var out []store.DefinitionResourceRecord
	for rows.Next() {
		var record store.DefinitionResourceRecord
		var kind string
		if err := rows.Scan(
			&record.CanonicalURL, &record.Version, &record.FHIRVersion, &record.FHIRResourceType,
			&kind, &record.Name, &record.Status, &record.PackageName, &record.PackageVersion,
			&record.ModuleName, &record.JSONData, &record.InstalledAt,
		); err != nil {
			return nil, fmt.Errorf("scan definition row: %w", err)
		}
		record.DefinitionKind = store.DefinitionKind(kind)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate definitions: %w", err)
	}
	return out, nil
}
