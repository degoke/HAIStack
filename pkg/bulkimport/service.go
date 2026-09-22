package bulkimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/binary"
	"github.com/degoke/haistack/pkg/core"
	"github.com/degoke/haistack/pkg/jobs"
	"github.com/degoke/haistack/pkg/store"
	"github.com/google/uuid"
)

// URLLoader optionally fetches remote NDJSON listed in Parameters input.url.
type URLLoader interface {
	Load(ctx context.Context, url string) ([]byte, error)
}

// Config configures a Service.
type Config struct {
	Jobs      JobStore
	Files     FileStore
	Executor  *Executor
	JobQueue  store.JobStore
	Loader    URLLoader
	BasePath  string
	PublicURL string
	Now       func() time.Time
	NewID     func() string
}

// Service orchestrates bulk import kickoff, polling, cancellation, and execution.
type Service struct {
	jobs      JobStore
	files     FileStore
	executor  *Executor
	jobQueue  store.JobStore
	loader    URLLoader
	basePath  string
	publicURL string
	now       func() time.Time
	newID     func() string
}

// NewService validates configuration and returns a Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.Jobs == nil {
		return nil, fmt.Errorf("import: JobStore is required")
	}
	if cfg.Files == nil {
		return nil, fmt.Errorf("import: FileStore is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("import: Executor is required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/fhir"
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = cfg.BasePath
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		cfg.NewID = uuid.NewString
	}
	return &Service{
		jobs:      cfg.Jobs,
		files:     cfg.Files,
		executor:  cfg.Executor,
		jobQueue:  cfg.JobQueue,
		loader:    cfg.Loader,
		basePath:  strings.TrimSuffix(cfg.BasePath, "/"),
		publicURL: strings.TrimSuffix(cfg.PublicURL, "/"),
		now:       cfg.Now,
		newID:     cfg.NewID,
	}, nil
}

// Kickoff creates an async import job and optionally enqueues background execution.
func (s *Service) Kickoff(ctx context.Context, req KickoffRequest) (*Job, error) {
	if s == nil {
		return nil, fmt.Errorf("import: service is nil")
	}
	if err := s.prepareInputs(ctx, &req); err != nil {
		return nil, err
	}
	id := s.newID()
	now := nowUTC(s.now)
	written := make([]string, 0, len(req.Inputs))
	for i, input := range req.Inputs {
		path := inputPath(id, i, input.Type)
		if err := s.files.Put(ctx, path, input.NDJSON, InputFormatNDJSON); err != nil {
			if delErr := s.deleteKickoffFiles(ctx, written); delErr != nil {
				return nil, fmt.Errorf("%w (cleanup: %v)", err, delErr)
			}
			return nil, err
		}
		written = append(written, path)
		req.Inputs[i].NDJSON = nil
	}
	job := Job{
		ID:        id,
		Status:    StatusInProgress,
		Request:   req,
		CreatedAt: now,
		Progress:  "0%",
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		if delErr := s.deleteKickoffFiles(ctx, written); delErr != nil {
			return nil, fmt.Errorf("%w (cleanup: %v)", err, delErr)
		}
		return nil, err
	}
	if s.jobQueue != nil {
		_, err := jobs.Enqueue(ctx, s.jobQueue, jobs.TypeImportBulk, JobPayload{JobID: id}, jobs.EnqueueOptions{
			Now: s.now,
		})
		if err != nil {
			if delErr := s.deleteKickoffFiles(ctx, written); delErr != nil {
				err = fmt.Errorf("%w (cleanup: %v)", err, delErr)
			}
			return nil, s.abortKickoff(ctx, &job, err)
		}
	} else if err := s.RunJob(ctx, id); err != nil {
		return nil, err
	}
	return s.jobs.Get(ctx, id)
}

func (s *Service) prepareInputs(ctx context.Context, req *KickoffRequest) error {
	if strings.TrimSpace(req.InputFormat) != "" && !strings.EqualFold(req.InputFormat, InputFormatNDJSON) {
		return invalidImport(fmt.Sprintf("import: unsupported inputFormat %q", req.InputFormat))
	}
	if len(req.Inputs) == 0 {
		return invalidImport("import: at least one input is required")
	}
	for i := range req.Inputs {
		input := &req.Inputs[i]
		if len(bytes.TrimSpace(input.NDJSON)) > 0 {
			continue
		}
		if strings.TrimSpace(input.URL) == "" {
			return invalidImport(fmt.Sprintf("import: input %d requires NDJSON or url", i))
		}
		if s.loader == nil {
			return invalidImport(fmt.Sprintf("import: input url %q cannot be fetched; provide inline NDJSON", input.URL))
		}
		data, err := s.loader.Load(ctx, input.URL)
		if err != nil {
			return invalidImport(fmt.Sprintf("import: load %s: %v", input.URL, err))
		}
		input.NDJSON = data
	}
	return nil
}

// GetJob returns the current job state.
func (s *Service) GetJob(ctx context.Context, id string) (*Job, error) {
	return s.jobs.Get(ctx, id)
}

// Cancel requests cancellation of an in-progress import job.
func (s *Service) Cancel(ctx context.Context, id string) error {
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("import: job %q not found", id)
	}
	switch job.Status {
	case StatusComplete, StatusError, StatusCancelled:
		return nil
	}
	job.CancelRequested = true
	job.Status = StatusCancelled
	job.CompletedAt = nowUTC(s.now)
	return s.jobs.Update(ctx, *job)
}

// Manifest builds the import manifest for a completed job.
func (s *Service) Manifest(job *Job) *Manifest {
	if job == nil {
		return nil
	}
	return &Manifest{
		TransactionTime:     job.TransactionTime,
		Request:             job.Request.RequestURL,
		RequiresAccessToken: job.Request.RequiresAccess,
		Output:              append([]CountFile(nil), job.Output...),
		Error:               append([]ErrorFile(nil), job.Errors...),
	}
}

// StatusURL returns the polling URL for a job.
func (s *Service) StatusURL(jobID string) string {
	return s.publicURL + "/$import/status/" + jobID
}

// FileURL returns the public URL prefix for import error artifacts.
func (s *Service) FileURL(jobID string) string {
	return s.publicURL + "/$import/files/" + jobID
}

// GetFile serves one import error artifact.
func (s *Service) GetFile(ctx context.Context, jobID, filename string) ([]byte, string, error) {
	path := jobID + "/" + strings.TrimPrefix(filename, "/")
	return s.files.Get(ctx, path)
}

// RunJob executes one import job synchronously.
func (s *Service) RunJob(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("import: job %q not found", jobID)
	}
	if job.Status != StatusInProgress {
		return nil
	}

	isCancelled := func() bool {
		current, getErr := s.jobs.Get(ctx, jobID)
		if getErr != nil || current == nil {
			return false
		}
		return current.CancelRequested || current.Status == StatusCancelled
	}

	result, err := s.executor.Execute(ctx, ExecuteRequest{
		JobID:       jobID,
		Inputs:      job.Request.Inputs,
		BaseFileURL: s.FileURL(jobID),
		IsCancelled: isCancelled,
		OnProgress: func(done, total int) {
			if total <= 0 {
				return
			}
			current, getErr := s.jobs.Get(ctx, jobID)
			if getErr != nil || current == nil {
				return
			}
			if current.Status == StatusCancelled || current.CancelRequested {
				return
			}
			current.Progress = fmt.Sprintf("%d%%", (done*100)/total)
			_ = s.jobs.Update(ctx, *current)
		},
	})
	if err != nil {
		current, getErr := s.jobs.Get(ctx, jobID)
		if getErr != nil {
			return getErr
		}
		if current == nil {
			return fmt.Errorf("import: job %q not found", jobID)
		}
		if current.CancelRequested || current.Status == StatusCancelled {
			current.Status = StatusCancelled
			current.CompletedAt = nowUTC(s.now)
			return s.jobs.Update(ctx, *current)
		}
		return s.failJob(ctx, current, err)
	}

	return s.completeJob(ctx, jobID, result)
}

func (s *Service) completeJob(ctx context.Context, jobID string, result *ExecuteResult) error {
	current, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("import: job %q not found", jobID)
	}
	if current.Status == StatusCancelled || current.CancelRequested {
		current.Status = StatusCancelled
		current.CompletedAt = nowUTC(s.now)
		return s.jobs.Update(ctx, *current)
	}
	current.Status = StatusComplete
	current.TransactionTime = nowUTC(s.now)
	current.CompletedAt = current.TransactionTime
	current.Progress = "100%"
	if result != nil {
		current.Output = result.Output
		current.Errors = result.Errors
	}
	return s.jobs.Update(ctx, *current)
}

func (s *Service) failJob(ctx context.Context, job *Job, cause error) error {
	job.Status = StatusError
	job.LastError = cause.Error()
	job.CompletedAt = nowUTC(s.now)
	return s.jobs.Update(ctx, *job)
}

type jobDeleter interface {
	Delete(ctx context.Context, id string) error
}

func (s *Service) abortKickoff(ctx context.Context, job *Job, cause error) error {
	if deleter, ok := s.jobs.(jobDeleter); ok {
		if err := deleter.Delete(ctx, job.ID); err != nil {
			return fmt.Errorf("import: enqueue: %w (delete status: %v)", cause, err)
		}
		return cause
	}
	if markErr := s.failJob(ctx, job, cause); markErr != nil {
		return fmt.Errorf("import: enqueue: %w (mark failed: %v)", cause, markErr)
	}
	return cause
}

func (s *Service) deleteKickoffFiles(ctx context.Context, paths []string) error {
	if s == nil || s.files == nil {
		return nil
	}
	var errs []error
	for _, path := range paths {
		if err := s.files.Delete(ctx, path); err != nil && !errors.Is(err, binary.ErrNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// JobHandler returns a jobs.Handler that executes bulk import jobs.
func (s *Service) JobHandler() jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload JobPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		return s.RunJob(ctx, payload.JobID)
	})
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func invalidImport(message string) error {
	return &core.ServiceError{Kind: core.ErrorKindInvalid, Message: message}
}
