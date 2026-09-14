package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// Progress reports durable job execution state stored inside JobRecord.Payload.
type Progress struct {
	Phase   string `json:"phase,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
}

// Reporter updates running job progress without changing job status.
type Reporter struct {
	Store store.JobStore
	Job   store.JobRecord
}

// NewReporter constructs a progress reporter for a claimed job.
func NewReporter(st store.JobStore, job store.JobRecord) *Reporter {
	return &Reporter{Store: st, Job: job}
}

// Update merges progress into the job payload and persists the job record.
func (r *Reporter) Update(ctx context.Context, progress Progress) error {
	if r == nil || r.Store == nil {
		return nil
	}
	payload, err := mergeProgress(r.Job.Payload, progress)
	if err != nil {
		return err
	}
	r.Job.Payload = payload
	r.Job.UpdatedAt = time.Now().UTC()
	return r.Store.Update(ctx, r.Job)
}

// Complete stores a terminal result object in the payload.
func (r *Reporter) Complete(ctx context.Context, result any) error {
	if r == nil || r.Store == nil {
		return nil
	}
	payload, err := mergeResult(r.Job.Payload, result)
	if err != nil {
		return err
	}
	r.Job.Payload = payload
	r.Job.UpdatedAt = time.Now().UTC()
	return r.Store.Update(ctx, r.Job)
}

// ProgressFromPayload extracts progress from a job payload, if present.
func ProgressFromPayload(payload []byte) (*Progress, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var meta struct {
		Progress *Progress `json:"progress"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return nil, fmt.Errorf("decode job progress: %w", err)
	}
	return meta.Progress, nil
}

// ResultFromPayload extracts a stored result object from the payload.
func ResultFromPayload(payload []byte) (map[string]any, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var meta struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return nil, fmt.Errorf("decode job result: %w", err)
	}
	return meta.Result, nil
}

func mergeProgress(payload []byte, progress Progress) ([]byte, error) {
	meta := map[string]any{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &meta); err != nil {
			return nil, fmt.Errorf("decode job payload: %w", err)
		}
	}
	meta["progress"] = progress
	return json.Marshal(meta)
}

func mergeResult(payload []byte, result any) ([]byte, error) {
	meta := map[string]any{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &meta); err != nil {
			return nil, fmt.Errorf("decode job payload: %w", err)
		}
	}
	meta["result"] = result
	return json.Marshal(meta)
}
