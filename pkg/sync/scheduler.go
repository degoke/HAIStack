package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// JobProcessor executes sync background jobs against an Engine.
type JobProcessor struct {
	Engine *Engine
	Jobs   store.JobStore
	Clock  Clock
}

// ProcessNext claims and runs one pending sync job of any supported type.
func (p *JobProcessor) ProcessNext(ctx context.Context) (bool, error) {
	if p == nil || p.Engine == nil || p.Jobs == nil {
		return false, fmt.Errorf("job processor requires engine and job store")
	}

	for _, jobType := range []string{
		JobTypeRetryPush,
		JobTypeScheduledPull,
		JobTypeConflictProcessing,
		JobTypeEventReplay,
	} {
		job, err := p.Jobs.ClaimNext(ctx, jobType)
		if err != nil {
			return false, err
		}
		if job == nil {
			continue
		}
		if err := p.runJob(ctx, *job); err != nil {
			job.Status = store.JobStatusFailed
			job.LastError = err.Error()
			job.Attempts++
			job.UpdatedAt = p.now()
			_ = p.Jobs.Update(ctx, *job)
			return true, err
		}
		job.Status = store.JobStatusCompleted
		job.UpdatedAt = p.now()
		if err := p.Jobs.Update(ctx, *job); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func (p *JobProcessor) runJob(ctx context.Context, job store.JobRecord) error {
	switch job.Type {
	case JobTypeRetryPush:
		_, err := p.Engine.Push(ctx)
		return err
	case JobTypeScheduledPull:
		_, err := p.Engine.Pull(ctx)
		return err
	case JobTypeConflictProcessing:
		return p.processConflictJob(ctx, job)
	case JobTypeEventReplay:
		return p.processReplayJob(ctx, job)
	default:
		return fmt.Errorf("unsupported sync job type %q", job.Type)
	}
}

func (p *JobProcessor) processConflictJob(ctx context.Context, job store.JobRecord) error {
	var payload ConflictJobPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode conflict job payload: %w", err)
		}
	}
	_ = payload
	// Detailed merge policy belongs in haistack-conflict; v1 records are already persisted.
	return nil
}

func (p *JobProcessor) processReplayJob(ctx context.Context, job store.JobRecord) error {
	var payload ReplayJobPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode replay job payload: %w", err)
		}
	}
	if payload.CanonicalSeq > 0 {
		_, err := p.Engine.Pull(ctx)
		return err
	}
	_, err := p.Engine.Push(ctx)
	return err
}

// EnqueueScheduledPull schedules a future pull attempt.
func EnqueueScheduledPull(ctx context.Context, jobs store.JobStore, nodeID, tenantID string, runAfter time.Time) error {
	if jobs == nil {
		return nil
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(ReplayJobPayload{
		NodeID:   nodeID,
		TenantID: tenantID,
		Reason:   "scheduled pull",
	})
	return jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      JobTypeScheduledPull,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
		RunAfter:  runAfter,
	})
}

func (p *JobProcessor) now() time.Time {
	if p != nil && p.Clock != nil {
		return p.Clock()
	}
	return DefaultClock()
}
