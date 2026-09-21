package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// IsMissing reports whether err means the job id is absent.
// It accepts ErrJobNotFound and the "job not found:" dialect used by
// pkg/postgres and pkg/sqlite, without treating unrelated errors as missing.
func IsMissing(err error) bool {
	return missingError(err, "job")
}

func missingError(err error, noun string) bool {
	if err == nil {
		return false
	}
	if noun == "job" && errors.Is(err, ErrJobNotFound) {
		return true
	}
	msg := err.Error()
	token := noun + " not found"
	return msg == token || strings.HasPrefix(msg, token+":") || strings.Contains(msg, ": "+token)
}

// GetRecord loads a job row of expectedType. Missing ids and type mismatches
// return (nil, nil). Persistence errors are returned as-is.
func GetRecord(ctx context.Context, db store.JobStore, expectedType, id string) (*store.JobRecord, error) {
	if db == nil {
		return nil, ErrNilStore
	}
	record, err := db.Get(ctx, id)
	if err != nil {
		if IsMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if record == nil || record.Type != expectedType {
		return nil, nil
	}
	return record, nil
}

// Lookup decodes a typed payload from a status record. Missing records return (nil, nil).
func Lookup[T any](ctx context.Context, db store.JobStore, expectedType, id string) (*T, error) {
	record, err := GetRecord(ctx, db, expectedType, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var value T
	if err := UnmarshalPayload(record.Payload, &value); err != nil {
		return nil, fmt.Errorf("jobs: decode %s %q: %w", expectedType, id, err)
	}
	return &value, nil
}

// WriteRecord updates payload, mapped status, and lastError on an existing row.
func WriteRecord(ctx context.Context, db store.JobStore, record *store.JobRecord, payload any, status store.JobStatus, lastError string) error {
	if db == nil {
		return ErrNilStore
	}
	if record == nil {
		return ErrJobNotFound
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		return fmt.Errorf("jobs: encode %s %q: %w", record.Type, record.ID, err)
	}
	record.Payload = body
	record.Status = status
	record.LastError = lastError
	record.UpdatedAt = time.Now().UTC()
	return db.Update(ctx, *record)
}

// Guard merges an incoming status-row update with the persisted value.
type Guard[T any] func(existing, incoming T) T

// StatusStore persists typed FHIR job records in store.JobStore.
// Update holds a mutex so cancel-wins guards cannot race with complete writes
// in-process (the same protection InMemoryJobStore gets from its lock).
type StatusStore[T any] struct {
	mu  sync.Mutex
	db  store.JobStore
	typ string
}

// NewStatusStore wraps db for records of typ (for example TypeExportBulkRecord).
func NewStatusStore[T any](db store.JobStore, typ string) *StatusStore[T] {
	return &StatusStore[T]{db: db, typ: typ}
}

// Create enqueues a new status row. It is not claimed by workers of other types.
func (s *StatusStore[T]) Create(ctx context.Context, id string, job T, status store.JobStatus, lastError string, createdAt time.Time) error {
	if s == nil || s.db == nil {
		return ErrNilStore
	}
	if id == "" {
		return ErrEmptyJobID
	}
	record, err := NewJob(s.typ, job, EnqueueOptions{ID: id, Now: createdAtNow(createdAt)})
	if err != nil {
		return err
	}
	record.Status = status
	record.LastError = lastError
	return s.db.Enqueue(ctx, record)
}

// Get returns the decoded status row, or (nil, nil) when missing.
func (s *StatusStore[T]) Get(ctx context.Context, id string) (*T, error) {
	if s == nil || s.db == nil {
		return nil, ErrNilStore
	}
	return Lookup[T](ctx, s.db, s.typ, id)
}

// Update applies guard under a mutex, then writes mapped store status from meta.
func (s *StatusStore[T]) Update(ctx context.Context, id string, incoming T, guard Guard[T], meta func(T) (store.JobStatus, string)) error {
	if s == nil || s.db == nil {
		return ErrNilStore
	}
	if id == "" {
		return ErrEmptyJobID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := GetRecord(ctx, s.db, s.typ, id)
	if err != nil {
		return err
	}
	if record == nil {
		return ErrJobNotFound
	}
	var existing T
	if err := UnmarshalPayload(record.Payload, &existing); err != nil {
		return fmt.Errorf("jobs: decode %s %q: %w", s.typ, id, err)
	}
	job := incoming
	if guard != nil {
		job = guard(existing, incoming)
	}
	status := store.JobStatusRunning
	var lastError string
	if meta != nil {
		status, lastError = meta(job)
	}
	return WriteRecord(ctx, s.db, record, job, status, lastError)
}

func createdAtNow(created time.Time) func() time.Time {
	if created.IsZero() {
		return nil
	}
	return func() time.Time { return created }
}
