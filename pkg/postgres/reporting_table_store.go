package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReportingTableStore persists tenant-scoped analytics reporting tables in Postgres.
type ReportingTableStore struct {
	pool     *pgxpool.Pool
	tenantID string
}

func newReportingTableStore(pool *pgxpool.Pool, tenantID string) *ReportingTableStore {
	return &ReportingTableStore{pool: pool, tenantID: tenantID}
}

func (s *ReportingTableStore) Refresh(ctx context.Context, meta store.ReportingTableMeta, rows []map[string]any) error {
	if err := s.ensureTenant(ctx); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reporting refresh: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, `
		DELETE FROM analytics_reporting_row
		WHERE tenant_id = $1 AND view_name = $2 AND view_version = $3`,
		s.tenantID, meta.ViewName, meta.ViewVersion,
	); err != nil {
		return fmt.Errorf("delete reporting rows: %w", err)
	}

	columnsJSON, err := json.Marshal(meta.Columns)
	if err != nil {
		return fmt.Errorf("marshal reporting columns: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO analytics_reporting_meta (tenant_id, view_name, view_version, columns, row_count, refreshed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, view_name, view_version) DO UPDATE SET
			columns = EXCLUDED.columns,
			row_count = EXCLUDED.row_count,
			refreshed_at = EXCLUDED.refreshed_at`,
		s.tenantID, meta.ViewName, meta.ViewVersion, columnsJSON, len(rows), meta.RefreshedAt,
	); err != nil {
		return fmt.Errorf("upsert reporting meta: %w", err)
	}

	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("marshal reporting row %d: %w", i, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO analytics_reporting_row (tenant_id, view_name, view_version, row_num, data)
			VALUES ($1, $2, $3, $4, $5)`,
			s.tenantID, meta.ViewName, meta.ViewVersion, int64(i), data,
		); err != nil {
			return fmt.Errorf("insert reporting row %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reporting refresh: %w", err)
	}
	committed = true
	return nil
}

func (s *ReportingTableStore) QueryRows(ctx context.Context, viewName, viewVersion string) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT data FROM analytics_reporting_row
		WHERE tenant_id = $1 AND view_name = $2 AND view_version = $3
		ORDER BY row_num ASC`,
		s.tenantID, viewName, viewVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("query reporting rows: %w", err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var dataJSON []byte
		if err := rows.Scan(&dataJSON); err != nil {
			return nil, fmt.Errorf("scan reporting row: %w", err)
		}
		var row map[string]any
		if err := json.Unmarshal(dataJSON, &row); err != nil {
			return nil, fmt.Errorf("unmarshal reporting row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reporting rows: %w", err)
	}
	return out, nil
}

func (s *ReportingTableStore) GetMeta(ctx context.Context, viewName, viewVersion string) (*store.ReportingTableMeta, error) {
	var (
		meta        store.ReportingTableMeta
		columnsJSON []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT view_name, view_version, columns, row_count, refreshed_at
		FROM analytics_reporting_meta
		WHERE tenant_id = $1 AND view_name = $2 AND view_version = $3`,
		s.tenantID, viewName, viewVersion,
	).Scan(&meta.ViewName, &meta.ViewVersion, &columnsJSON, &meta.RowCount, &meta.RefreshedAt)
	if isNoRows(err) {
		return nil, fmt.Errorf("reporting table not found: %s/%s", viewName, viewVersion)
	}
	if err != nil {
		return nil, fmt.Errorf("get reporting meta: %w", err)
	}
	if len(columnsJSON) > 0 {
		if err := json.Unmarshal(columnsJSON, &meta.Columns); err != nil {
			return nil, fmt.Errorf("unmarshal reporting columns: %w", err)
		}
	}
	return &meta, nil
}

func (s *ReportingTableStore) ensureTenant(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, s.tenantID)
	if err != nil {
		return fmt.Errorf("ensure tenant %q: %w", s.tenantID, err)
	}
	return nil
}

var _ store.ReportingTableStore = (*ReportingTableStore)(nil)
