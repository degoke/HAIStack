package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// EnqueueOptions customize NewJob / Enqueue defaults.
type EnqueueOptions struct {
	ID       string
	RunAfter time.Time
	Now      func() time.Time
	NewID    func() string
}

// MarshalPayload JSON-encodes a typed payload for JobRecord.Payload.
func MarshalPayload(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if b, ok := v.([]byte); ok {
		return append([]byte(nil), b...), nil
	}
	return json.Marshal(v)
}

// UnmarshalPayload JSON-decodes JobRecord.Payload into dest.
func UnmarshalPayload(payload []byte, dest any) error {
	if len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, dest)
}

// NewJob builds a pending JobRecord with id/timestamp defaults.
func NewJob(jobType string, payload any, opts EnqueueOptions) (store.JobRecord, error) {
	if jobType == "" {
		return store.JobRecord{}, ErrEmptyJobType
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		return store.JobRecord{}, fmt.Errorf("jobs: marshal payload: %w", err)
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	id := opts.ID
	if id == "" {
		if opts.NewID != nil {
			id = opts.NewID()
		} else {
			id = uuid.NewString()
		}
	}
	return store.JobRecord{
		ID:        id,
		Type:      jobType,
		Payload:   body,
		Status:    store.JobStatusPending,
		Attempts:  0,
		CreatedAt: now,
		UpdatedAt: now,
		RunAfter:  opts.RunAfter,
	}, nil
}

// Enqueue builds a job with NewJob and persists it.
func Enqueue(ctx context.Context, jobs store.JobStore, jobType string, payload any, opts EnqueueOptions) (store.JobRecord, error) {
	if jobs == nil {
		return store.JobRecord{}, ErrNilStore
	}
	job, err := NewJob(jobType, payload, opts)
	if err != nil {
		return store.JobRecord{}, err
	}
	if err := jobs.Enqueue(ctx, job); err != nil {
		return store.JobRecord{}, err
	}
	return job, nil
}
