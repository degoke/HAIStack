package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncStatusReport is the local-store sync view returned by haistack sync status.
type SyncStatusReport struct {
	NodeID              string `json:"nodeId"`
	HubURL              string `json:"hubURL,omitempty"`
	LastPullCursor      string `json:"lastPullCursor,omitempty"`
	PendingRetryPush    int    `json:"pendingRetryPushJobs"`
	UnresolvedConflicts int    `json:"unresolvedConflicts"`
}

type syncStatusReader interface {
	GetCursor(context.Context, string) (string, bool, error)
	CountPendingJobs(context.Context, string) (int, error)
	CountUnresolvedConflicts(context.Context) (int, error)
}

// ReadSyncStatus inspects local persistence for sync status fields.
func ReadSyncStatus(ctx context.Context, cfg config.Config, reader syncStatusReader) (*SyncStatusReport, error) {
	report := &SyncStatusReport{
		NodeID: cfg.Sync.NodeID,
		HubURL: cfg.Sync.HubURL,
	}
	pos, ok, err := reader.GetCursor(ctx, hasync.CursorPull)
	if err != nil {
		return nil, err
	}
	if ok {
		report.LastPullCursor = pos
	}
	report.PendingRetryPush, err = reader.CountPendingJobs(ctx, jobs.TypeSyncRetryPush)
	if err != nil {
		return nil, err
	}
	report.UnresolvedConflicts, err = reader.CountUnresolvedConflicts(ctx)
	if err != nil {
		return nil, err
	}
	return report, nil
}

// SQLiteStatusReader reads sync status fields from SQLite.
type SQLiteStatusReader struct {
	DB *sql.DB
}

func (r SQLiteStatusReader) GetCursor(ctx context.Context, name string) (string, bool, error) {
	var position sql.NullString
	err := r.DB.QueryRowContext(ctx, `SELECT position FROM hai_sync_cursor WHERE name = ?`, name).Scan(&position)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read cursor %s: %w", name, err)
	}
	if !position.Valid {
		return "", true, nil
	}
	return position.String, true, nil
}

func (r SQLiteStatusReader) CountPendingJobs(ctx context.Context, jobType string) (int, error) {
	var count int
	err := r.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM hai_background_job
		WHERE type = ? AND status = 'pending'`, jobType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending jobs: %w", err)
	}
	return count, nil
}

func (r SQLiteStatusReader) CountUnresolvedConflicts(ctx context.Context) (int, error) {
	var count int
	err := r.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM hai_sync_conflict WHERE resolved_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unresolved conflicts: %w", err)
	}
	return count, nil
}

// PostgresStatusReader reads tenant-scoped sync status fields from Postgres.
type PostgresStatusReader struct {
	Pool     *pgxpool.Pool
	TenantID string
}

func (r PostgresStatusReader) GetCursor(ctx context.Context, name string) (string, bool, error) {
	var position *string
	err := r.Pool.QueryRow(ctx, `
		SELECT position FROM hai_sync_cursor WHERE tenant_id = $1 AND name = $2`,
		r.TenantID, name).Scan(&position)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read cursor %s: %w", name, err)
	}
	if position == nil {
		return "", true, nil
	}
	return *position, true, nil
}

func (r PostgresStatusReader) CountPendingJobs(ctx context.Context, jobType string) (int, error) {
	var count int
	err := r.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM hai_background_job
		WHERE tenant_id = $1 AND type = $2 AND status = 'pending'`,
		r.TenantID, jobType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending jobs: %w", err)
	}
	return count, nil
}

func (r PostgresStatusReader) CountUnresolvedConflicts(ctx context.Context) (int, error) {
	var count int
	err := r.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM hai_sync_conflict
		WHERE tenant_id = $1 AND resolved_at IS NULL`, r.TenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unresolved conflicts: %w", err)
	}
	return count, nil
}

// StatusReaderForSession returns the appropriate sync status reader for a session.
func StatusReaderForSession(s *Session) (syncStatusReader, error) {
	p := s.Runtime.Persistence()
	if p.SQLite != nil {
		return SQLiteStatusReader{DB: p.SQLite.SQL()}, nil
	}
	if p.Postgres != nil && p.TenantDB != nil {
		return PostgresStatusReader{
			Pool:     p.Postgres.Pool(),
			TenantID: p.TenantDB.TenantID(),
		}, nil
	}
	return nil, fmt.Errorf("no persistence backend available for sync status")
}

// FormatSyncStatusText renders a human-readable sync status summary.
func FormatSyncStatusText(report *SyncStatusReport) string {
	pull := report.LastPullCursor
	if pull == "" {
		pull = "(none)"
	}
	hub := report.HubURL
	if hub == "" {
		hub = "(not configured)"
	}
	return fmt.Sprintf(
		"node: %s\nhub: %s\nlast pull cursor: %s\npending retry-push jobs: %d\nunresolved conflicts: %d",
		report.NodeID, hub, pull, report.PendingRetryPush, report.UnresolvedConflicts,
	)
}
