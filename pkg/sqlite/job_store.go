package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// JobStore persists background jobs in SQLite.
type JobStore struct {
	db *sql.DB
}

func newJobStore(db *sql.DB) *JobStore {
	return &JobStore{db: db}
}

var _ store.JobStore = (*JobStore)(nil)

// Enqueue implements store.JobStore.
func (s *JobStore) Enqueue(ctx context.Context, job store.JobRecord) error {
	var runAfter any
	if !job.RunAfter.IsZero() {
		runAfter = formatTime(job.RunAfter)
	}
	var lastError any
	if job.LastError != "" {
		lastError = job.LastError
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO background_job (
			id, type, payload, status, attempts, created_at, updated_at, run_after, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Type, job.Payload, string(job.Status), job.Attempts,
		formatTime(job.CreatedAt), formatTime(job.UpdatedAt), runAfter, lastError,
	)
	if err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	return nil
}

// ClaimNext implements store.JobStore with Postgres-compatible claim semantics.
func (s *JobStore) ClaimNext(ctx context.Context, jobType string) (*store.JobRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim job: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	nowStr := formatTime(now)

	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM background_job
		WHERE type = ?
			AND status = 'pending'
			AND (run_after IS NULL OR run_after <= ?)
		ORDER BY created_at ASC, id ASC
		LIMIT 1`,
		jobType, nowStr,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select next job: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE background_job
		SET status = 'running', attempts = attempts + 1, updated_at = ?
		WHERE id = ? AND status = 'pending'`,
		nowStr, id,
	)
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim job rows: %w", err)
	}
	if affected == 0 {
		return nil, nil
	}

	job, err := scanJob(tx.QueryRowContext(ctx, `
		SELECT id, type, payload, status, attempts, created_at, updated_at, run_after, last_error
		FROM background_job WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim job: %w", err)
	}
	return job, nil
}

// Update implements store.JobStore.
func (s *JobStore) Update(ctx context.Context, job store.JobRecord) error {
	var runAfter any
	if !job.RunAfter.IsZero() {
		runAfter = formatTime(job.RunAfter)
	}
	var lastError any
	if job.LastError != "" {
		lastError = job.LastError
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE background_job
		SET type = ?, payload = ?, status = ?, attempts = ?, updated_at = ?, run_after = ?, last_error = ?
		WHERE id = ?`,
		job.Type, job.Payload, string(job.Status), job.Attempts, formatTime(job.UpdatedAt),
		runAfter, lastError, job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update job rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	return nil
}

// Get implements store.JobStore.
func (s *JobStore) Get(ctx context.Context, id string) (*store.JobRecord, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `
		SELECT id, type, payload, status, attempts, created_at, updated_at, run_after, last_error
		FROM background_job WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(row jobScanner) (*store.JobRecord, error) {
	var (
		job       store.JobRecord
		status    string
		createdAt string
		updatedAt string
		runAfter  sql.NullString
		lastError sql.NullString
		payload   []byte
	)
	if err := row.Scan(
		&job.ID, &job.Type, &payload, &status, &job.Attempts,
		&createdAt, &updatedAt, &runAfter, &lastError,
	); err != nil {
		return nil, err
	}
	job.Payload = payload
	job.Status = store.JobStatus(status)
	var err error
	if job.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse job created_at: %w", err)
	}
	if job.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse job updated_at: %w", err)
	}
	if runAfter.Valid && runAfter.String != "" {
		if job.RunAfter, err = parseTime(runAfter.String); err != nil {
			return nil, fmt.Errorf("parse job run_after: %w", err)
		}
	}
	if lastError.Valid {
		job.LastError = lastError.String
	}
	return &job, nil
}
