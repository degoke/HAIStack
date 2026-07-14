package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/jobs"
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

// ProcessNext claims and runs one pending sync job of any supported type using
// the shared jobs.Runner runtime.
func (p *JobProcessor) ProcessNext(ctx context.Context) (bool, error) {
	if p == nil || p.Engine == nil || p.Jobs == nil {
		return false, fmt.Errorf("job processor requires engine and job store")
	}
	return p.runner().RunOnce(ctx)
}

func (p *JobProcessor) runner() *jobs.Runner {
	r := jobs.NewRunner(p.Jobs)
	r.MaxAttempts = 1
	r.Now = p.now
	_ = r.Register(JobTypeRetryPush, jobs.HandlerFunc(func(ctx context.Context, _ store.JobRecord) error {
		_, err := p.Engine.Push(ctx)
		return err
	}))
	_ = r.Register(JobTypeScheduledPull, jobs.HandlerFunc(func(ctx context.Context, _ store.JobRecord) error {
		_, err := p.Engine.Pull(ctx)
		return err
	}))
	_ = r.Register(JobTypeConflictProcessing, jobs.HandlerFunc(p.processConflictJob))
	_ = r.Register(JobTypeEventReplay, jobs.HandlerFunc(p.processReplayJob))
	return r
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
	appendAudit(ctx, cfg.Audit, audit.SyncEvent{
		ID:           uuid.NewString(),
		Timestamp:    p.now(),
		Actor:        payload.NodeID,
		Tenant:       payload.TenantID,
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
func EnqueueScheduledPull(ctx context.Context, jobStore store.JobStore, nodeID, tenantID string, runAfter time.Time) error {
	if jobStore == nil {
		return nil
	}
	_, err := jobs.Enqueue(ctx, jobStore, JobTypeScheduledPull, ReplayJobPayload{
		NodeID:   nodeID,
		TenantID: tenantID,
		Reason:   "scheduled pull",
	}, jobs.EnqueueOptions{RunAfter: runAfter})
	return err
}

func (p *JobProcessor) now() time.Time {
	if p != nil && p.Clock != nil {
		return p.Clock()
	}
	return DefaultClock()
}
