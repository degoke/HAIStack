package export

import (
	"context"
	"errors"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

const blobKeyPrefix = "bulk-export"

// DurableJobStore persists FHIR bulk export jobs in store.JobStore.
// Records use jobs.TypeExportBulkRecord so worker ClaimNext(TypeExportBulk)
// never claims status rows.
type DurableJobStore struct {
	records *jobs.StatusStore[Job]
}

// NewDurableJobStore wraps a database-backed job store.
func NewDurableJobStore(db store.JobStore) *DurableJobStore {
	return &DurableJobStore{records: jobs.NewStatusStore[Job](db, jobs.TypeExportBulkRecord)}
}

var _ JobStore = (*DurableJobStore)(nil)

func (s *DurableJobStore) Create(ctx context.Context, job Job) error {
	if s == nil || s.records == nil {
		return fmt.Errorf("export: job store is required")
	}
	if job.ID == "" {
		return fmt.Errorf("export: job id is required")
	}
	return s.records.Create(ctx, job.ID, job, recordStatus(job.Status), job.LastError, job.CreatedAt)
}

func (s *DurableJobStore) Get(ctx context.Context, id string) (*Job, error) {
	if s == nil || s.records == nil {
		return nil, fmt.Errorf("export: job store is required")
	}
	return s.records.Get(ctx, id)
}

func (s *DurableJobStore) Update(ctx context.Context, job Job) error {
	if s == nil || s.records == nil {
		return fmt.Errorf("export: job store is required")
	}
	if job.ID == "" {
		return fmt.Errorf("export: job id is required")
	}
	err := s.records.Update(ctx, job.ID, job, applyCancelGuard, func(j Job) (store.JobStatus, string) {
		return recordStatus(j.Status), j.LastError
	})
	if errors.Is(err, jobs.ErrJobNotFound) {
		return fmt.Errorf("export: job %q not found", job.ID)
	}
	return err
}

// NewBlobFileStore wraps a blob store for export artifacts.
func NewBlobFileStore(blobs store.BlobStore) FileStore {
	return binary.NewPrefixedFileStore(blobs, blobKeyPrefix, "export", "application/fhir+ndjson")
}

func recordStatus(status JobStatus) store.JobStatus {
	switch status {
	case StatusComplete, StatusCancelled:
		return store.JobStatusCompleted
	case StatusError:
		return store.JobStatusFailed
	default:
		return store.JobStatusRunning
	}
}

func applyCancelGuard(existing, incoming Job) Job {
	if existing.Status != StatusCancelled && !existing.CancelRequested {
		return incoming
	}
	if incoming.Status != StatusCancelled {
		existing.Status = StatusCancelled
		existing.CancelRequested = true
		return existing
	}
	incoming.Status = StatusCancelled
	incoming.CancelRequested = true
	return incoming
}
