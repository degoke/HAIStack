package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
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
	if p.Engine == nil || p.Engine.Config.ConflictEngine == nil {
		// Conflict engine is optional; when absent, the record remains for manual review.
		return nil
	}

	local, err := localEventFromPayload(payload)
	if err != nil {
		return err
	}

	cfg := p.Engine.Config.normalized()
	base, current, err := resolveBaseAndCurrent(ctx, cfg, local)
	if err != nil {
		return err
	}

	mergeResult := p.Engine.Config.ConflictEngine.Merge(local, base, current)
	if !mergeResult.AutoMergeable {
		p.appendConflictAudit(ctx, cfg, payload, AuditConflictNeedsReview, map[string]string{
			"classification": string(mergeResult.Result.Classification),
			"risk":           string(mergeResult.Result.Risk),
			"reason":         mergeResult.Result.ReviewReason,
		})
		p.notifyResolutionHandler(ctx, cfg, payload, mergeResult)
		return nil
	}

	p.appendConflictAudit(ctx, cfg, payload, AuditConflictAutoMerged, map[string]string{
		"resolution": string(mergeResult.Resolution),
	})
	p.notifyResolutionHandler(ctx, cfg, payload, mergeResult)

	if cfg.Conflicts != nil && payload.ConflictID != "" {
		_ = cfg.Conflicts.Resolve(ctx, payload.ConflictID, p.now())
	}
	return nil
}

func (p *JobProcessor) notifyResolutionHandler(
	ctx context.Context,
	cfg Config,
	payload ConflictJobPayload,
	result conflict.MergeResult,
) {
	if cfg.ConflictResolutionHandler == nil {
		return
	}
	_ = cfg.ConflictResolutionHandler.OnConflictResolution(ctx, payload, result)
}

func localEventFromPayload(payload ConflictJobPayload) (conflict.LocalEvent, error) {
	if len(payload.LocalEvent) > 0 {
		var syncEvent LocalEvent
		if err := json.Unmarshal(payload.LocalEvent, &syncEvent); err != nil {
			return conflict.LocalEvent{}, fmt.Errorf("decode embedded local event: %w", err)
		}
		return conflict.LocalEvent{
			EventID:          syncEvent.EventID,
			ResourceType:     syncEvent.ResourceType,
			ResourceID:       syncEvent.ResourceID,
			Operation:        string(syncEvent.Operation),
			BaseCloudVersion: syncEvent.BaseCloudVersion,
			LocalVersion:     syncEvent.LocalVersion,
			ChangedPaths:     syncEvent.ChangedPaths,
			Patch:            syncEvent.Patch,
			ResourceAfter:    syncEvent.ResourceAfter,
			ResourceHash:     syncEvent.ResourceHash,
		}, nil
	}
	return conflict.LocalEvent{
		ResourceType: payload.ResourceType,
		ResourceID:   payload.ResourceID,
		Operation:    string(EventTypeResourceUpdated),
		LocalVersion: payload.LocalVersionID,
	}, nil
}

func resolveBaseAndCurrent(
	ctx context.Context,
	cfg Config,
	local conflict.LocalEvent,
) (*types.ResourceEnvelope, *types.ResourceEnvelope, error) {
	if cfg.Resources == nil {
		return nil, nil, fmt.Errorf("resource store required for conflict processing")
	}
	current, err := cfg.Resources.Read(ctx, local.ResourceType, local.ResourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("read current resource: %w", err)
	}
	if cfg.History == nil || local.BaseCloudVersion == "" {
		return nil, current, nil
	}
	versions, err := cfg.History.GetHistory(ctx, local.ResourceType, local.ResourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("read history: %w", err)
	}
	for _, v := range versions {
		if v.VersionID == local.BaseCloudVersion && v.Resource != nil {
			base := *v.Resource
			return &base, current, nil
		}
	}
	return nil, current, nil
}

func (p *JobProcessor) appendConflictAudit(
	ctx context.Context,
	cfg Config,
	payload ConflictJobPayload,
	action string,
	details map[string]string,
) {
	if cfg.Audit == nil {
		return
	}
	_ = cfg.Audit.Append(ctx, store.AuditRecord{
		ID:           uuid.NewString(),
		Timestamp:    p.now(),
		Actor:        payload.NodeID,
		Action:       action,
		ResourceType: payload.ResourceType,
		ResourceID:   payload.ResourceID,
		Outcome:      action,
		Details:      details,
	})
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
