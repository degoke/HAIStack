package bulkimport

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

const blobKeyPrefix = "bulk-import"

// DurableJobStore persists FHIR bulk import jobs in store.JobStore.
// Records use jobs.TypeImportBulkRecord so worker ClaimNext(TypeImportBulk)
// never claims status rows.
type DurableJobStore struct {
	db store.JobStore
}

// NewDurableJobStore wraps a database-backed job store.
func NewDurableJobStore(db store.JobStore) *DurableJobStore {
	return &DurableJobStore{db: db}
}

var _ JobStore = (*DurableJobStore)(nil)

func (s *DurableJobStore) Create(ctx context.Context, job Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("import: job store is required")
	}
	if job.ID == "" {
		return fmt.Errorf("import: job id is required")
	}
	record, err := jobs.NewJob(jobs.TypeImportBulkRecord, job, jobs.EnqueueOptions{
		ID:  job.ID,
		Now: createdAtNow(job.CreatedAt),
	})
	if err != nil {
		return err
	}
	record.Status = recordStatus(job.Status)
	record.LastError = job.LastError
	return s.db.Enqueue(ctx, record)
}

func (s *DurableJobStore) Get(ctx context.Context, id string) (*Job, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("import: job store is required")
	}
	return jobs.Lookup[Job](ctx, s.db, jobs.TypeImportBulkRecord, id)
}

func (s *DurableJobStore) Update(ctx context.Context, job Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("import: job store is required")
	}
	if job.ID == "" {
		return fmt.Errorf("import: job id is required")
	}
	record, err := jobs.GetRecord(ctx, s.db, jobs.TypeImportBulkRecord, job.ID)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("import: job %q not found", job.ID)
	}
	var existing Job
	if err := jobs.UnmarshalPayload(record.Payload, &existing); err != nil {
		return fmt.Errorf("import: decode job %q: %w", job.ID, err)
	}
	job = applyCancelGuard(existing, job)
	return jobs.WriteRecord(ctx, s.db, record, job, recordStatus(job.Status), job.LastError)
}

// NewBlobFileStore wraps a blob store for import artifacts.
func NewBlobFileStore(blobs store.BlobStore) FileStore {
	return binary.NewPrefixedFileStore(blobs, blobKeyPrefix, "import", InputFormatNDJSON)
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

func createdAtNow(created time.Time) func() time.Time {
	if created.IsZero() {
		return nil
	}
	return func() time.Time { return created }
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
