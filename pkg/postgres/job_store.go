package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobStore persists background jobs in Postgres.
type JobStore struct {
	exec     querier
	tenantID string
}

func newJobStore(pool *pgxpool.Pool, tenantID string) *JobStore {
	return &JobStore{exec: pool, tenantID: tenantID}
}

func (s *JobStore) Enqueue(ctx context.Context, job store.JobRecord) error {
	_, err := s.exec.Exec(ctx, `
		INSERT INTO hai_background_job (
			id, tenant_id, type, payload, status, attempts, created_at, updated_at, run_after, last_error
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		job.ID, s.tenantID, job.Type, job.Payload, string(job.Status), job.Attempts,
		job.CreatedAt, job.UpdatedAt, nullTime(job.RunAfter), nullString(job.LastError),
	)
	if err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	return nil
}

func (s *JobStore) ClaimNext(ctx context.Context, jobType string) (*store.JobRecord, error) {
	var job store.JobRecord
	var status string
	var runAfter *time.Time
	var lastError *string
	err := s.exec.QueryRow(ctx, `
		WITH next AS (
			SELECT id FROM hai_background_job
			WHERE tenant_id = $1 AND type = $2 AND status = 'pending'
				AND (run_after IS NULL OR run_after <= now())
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE hai_background_job j
		SET status = 'running', attempts = j.attempts + 1, updated_at = now()
		FROM next
		WHERE j.id = next.id
		RETURNING j.id, j.type, j.payload, j.status, j.attempts, j.created_at, j.updated_at, j.run_after, j.last_error`,
		s.tenantID, jobType,
	).Scan(
		&job.ID, &job.Type, &job.Payload, &status, &job.Attempts,
		&job.CreatedAt, &job.UpdatedAt, &runAfter, &lastError,
	)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next job: %w", err)
	}
	job.Status = store.JobStatus(status)
	if runAfter != nil {
		job.RunAfter = *runAfter
	}
	if lastError != nil {
		job.LastError = *lastError
	}
	return &job, nil
}

func (s *JobStore) Update(ctx context.Context, job store.JobRecord) error {
	tag, err := s.exec.Exec(ctx, `
		UPDATE hai_background_job
		SET type = $1, payload = $2, status = $3, attempts = $4, updated_at = $5, run_after = $6, last_error = $7
		WHERE tenant_id = $8 AND id = $9`,
		job.Type, job.Payload, string(job.Status), job.Attempts, job.UpdatedAt,
		nullTime(job.RunAfter), nullString(job.LastError), s.tenantID, job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	return nil
}

func (s *JobStore) Get(ctx context.Context, id string) (*store.JobRecord, error) {
	var (
		job       store.JobRecord
		status    string
		runAfter  *time.Time
		lastError *string
	)
	err := s.exec.QueryRow(ctx, `
		SELECT id, type, payload, status, attempts, created_at, updated_at, run_after, last_error
		FROM hai_background_job
		WHERE tenant_id = $1 AND id = $2`, s.tenantID, id,
	).Scan(
		&job.ID, &job.Type, &job.Payload, &status, &job.Attempts,
		&job.CreatedAt, &job.UpdatedAt, &runAfter, &lastError,
	)
	if isNoRows(err) {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	job.Status = store.JobStatus(status)
	if runAfter != nil {
		job.RunAfter = *runAfter
	}
	if lastError != nil {
		job.LastError = *lastError
	}
	return &job, nil
}
