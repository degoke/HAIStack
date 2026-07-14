package jobs

import "errors"

var (
	// ErrNilStore is returned when a Runner or enqueue helper is used without a JobStore.
	ErrNilStore = errors.New("jobs: job store is required")
	// ErrNilHandler is returned when Register is called with a nil handler.
	ErrNilHandler = errors.New("jobs: handler is required")
	// ErrEmptyJobType is returned when a job type string is empty.
	ErrEmptyJobType = errors.New("jobs: job type is required")
	// ErrUnknownJobType is returned when a claimed job has no registered handler.
	ErrUnknownJobType = errors.New("jobs: unknown job type")
	// ErrJobNotFound is returned by InMemoryJobStore.Get when the id is absent.
	ErrJobNotFound = errors.New("jobs: job not found")
	// ErrDuplicateJob is returned when Enqueue receives an id that already exists.
	ErrDuplicateJob = errors.New("jobs: job already exists")
	// ErrEmptyJobID is returned when Enqueue receives a job without an id.
	ErrEmptyJobID = errors.New("jobs: job id is required")
)
