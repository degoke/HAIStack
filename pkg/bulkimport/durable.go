package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	record, err := s.db.Get(ctx, id)
	if jobRecordMissing(err) || record == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record.Type != jobs.TypeImportBulkRecord {
		return nil, nil
	}
	var job Job
	if err := jobs.UnmarshalPayload(record.Payload, &job); err != nil {
		return nil, fmt.Errorf("import: decode job %q: %w", id, err)
	}
	return &job, nil
}

func (s *DurableJobStore) Update(ctx context.Context, job Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("import: job store is required")
	}
	if job.ID == "" {
		return fmt.Errorf("import: job id is required")
	}
	record, err := s.db.Get(ctx, job.ID)
	if jobRecordMissing(err) || record == nil {
		return fmt.Errorf("import: job %q not found", job.ID)
	}
	if err != nil {
		return err
	}
	if record.Type != jobs.TypeImportBulkRecord {
		return fmt.Errorf("import: job %q not found", job.ID)
	}
	var existing Job
	if err := jobs.UnmarshalPayload(record.Payload, &existing); err != nil {
		return fmt.Errorf("import: decode job %q: %w", job.ID, err)
	}
	if existing.Status == StatusCancelled || existing.CancelRequested {
		if job.Status != StatusCancelled {
			existing.Status = StatusCancelled
			existing.CancelRequested = true
			job = existing
		} else {
			job.Status = StatusCancelled
			job.CancelRequested = true
		}
	}
	payload, err := jobs.MarshalPayload(job)
	if err != nil {
		return fmt.Errorf("import: encode job %q: %w", job.ID, err)
	}
	record.Payload = payload
	record.Status = recordStatus(job.Status)
	record.LastError = job.LastError
	record.UpdatedAt = time.Now().UTC()
	return s.db.Update(ctx, *record)
}

// BlobFileStore persists bulk NDJSON artifacts in a store.BlobStore
// (object storage when configured, otherwise the database blob store).
type BlobFileStore struct {
	blobs  store.BlobStore
	prefix string
}

// NewBlobFileStore wraps a blob store for import artifacts.
func NewBlobFileStore(blobs store.BlobStore) *BlobFileStore {
	return &BlobFileStore{blobs: blobs, prefix: blobKeyPrefix}
}

var _ FileStore = (*BlobFileStore)(nil)

func (s *BlobFileStore) Put(ctx context.Context, path string, data []byte, contentType string) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("import: blob store is required")
	}
	key, err := blobObjectKey(s.prefix, path)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if contentType == "" {
		contentType = InputFormatNDJSON
	}
	return s.blobs.Put(ctx, store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        int64(len(data)),
		Data:        append([]byte(nil), data...),
		CreatedAt:   time.Now().UTC(),
	})
}

func (s *BlobFileStore) Get(ctx context.Context, path string) ([]byte, string, error) {
	if s == nil || s.blobs == nil {
		return nil, "", fmt.Errorf("import: blob store is required")
	}
	key, err := blobObjectKey(s.prefix, path)
	if err != nil {
		return nil, "", fmt.Errorf("import: %w", err)
	}
	obj, err := s.blobs.Get(ctx, key)
	if err != nil {
		if blobObjectMissing(err) {
			return nil, "", fmt.Errorf("import: file %q not found", path)
		}
		return nil, "", err
	}
	if obj == nil {
		return nil, "", fmt.Errorf("import: file %q not found", path)
	}
	ct := obj.ContentType
	if ct == "" {
		ct = InputFormatNDJSON
	}
	return append([]byte(nil), obj.Data...), ct, nil
}

func (s *BlobFileStore) Delete(ctx context.Context, path string) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("import: blob store is required")
	}
	key, err := blobObjectKey(s.prefix, path)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if err := s.blobs.Delete(ctx, key); err != nil && !blobObjectMissing(err) {
		return err
	}
	return nil
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

func jobRecordMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, jobs.ErrJobNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func blobObjectMissing(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func blobObjectKey(prefix, path string) (string, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if cleaned == "" {
		return "", fmt.Errorf("file path is required")
	}
	if strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid file path %q", path)
	}
	return prefix + "/" + cleaned, nil
}
