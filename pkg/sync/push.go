package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// Pusher reads pending outbox events and proposes them to the hub.
type Pusher struct {
	Config Config
}

// PushResultSummary aggregates one push pass.
type PushResultSummary struct {
	Proposed int
	Results  []PushResult
	Cursor   int64
}

// Push reads local outbox events after the push cursor and sends them to the hub.
func (p *Pusher) Push(ctx context.Context) (*PushResultSummary, error) {
	cfg := p.Config.normalized()
	if cfg.Hub == nil {
		return nil, fmt.Errorf("hub is required")
	}
	if cfg.Events == nil {
		return nil, fmt.Errorf("event store is required")
	}

	afterSeq, err := readCursorPosition(ctx, cfg.Cursors, cfg.PushCursorName)
	if err != nil {
		return nil, err
	}

	events, err := cfg.Events.ReadSince(ctx, afterSeq, cfg.PushBatchSize)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return &PushResultSummary{Cursor: afterSeq}, nil
	}

	localEvents := make([]LocalEvent, 0, len(events))
	for _, event := range events {
		enriched, err := EnrichLocalEvent(ctx, event, cfg.NodeID, cfg.TenantID, cfg.History)
		if err != nil {
			return nil, err
		}
		localEvents = append(localEvents, enriched)
	}
	localEvents = orderPushBatch(localEvents)

	results, err := cfg.Hub.Push(ctx, localEvents)
	if err != nil {
		return nil, err
	}
	if len(results) != len(localEvents) {
		return nil, fmt.Errorf("hub returned %d results for %d events", len(results), len(localEvents))
	}

	summary := &PushResultSummary{
		Proposed: len(localEvents),
		Results:  results,
		Cursor:   afterSeq,
	}

	var lastHandledSeq = afterSeq
	for i, result := range results {
		event := localEvents[i]
		if err := p.handlePushResult(ctx, cfg, event, result); err != nil {
			return summary, err
		}
		if result.IsTerminal() && event.OutboxSequence > lastHandledSeq {
			lastHandledSeq = event.OutboxSequence
		} else if !result.IsTerminal() {
			break
		}
	}

	if lastHandledSeq > afterSeq {
		if err := upsertCursor(ctx, cfg.Cursors, cfg.PushCursorName, lastHandledSeq, cfg.Clock()); err != nil {
			return summary, err
		}
		summary.Cursor = lastHandledSeq
	}

	return summary, nil
}

func (p *Pusher) handlePushResult(ctx context.Context, cfg Config, event LocalEvent, result PushResult) error {
	now := cfg.Clock()

	appendAudit(ctx, cfg.Audit, audit.SyncEvent{
		ID:           uuid.NewString(),
		Timestamp:    now,
		Actor:        cfg.NodeID,
		Tenant:       cfg.TenantID,
		Action:       AuditDevicePushed,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Outcome:      string(result.State),
		Details: map[string]string{
			"eventId": event.EventID,
		},
	})

	switch result.State {
	case AckAccepted:
		appendAudit(ctx, cfg.Audit, audit.SyncEvent{
			ID:           uuid.NewString(),
			Timestamp:    now,
			Actor:        cfg.NodeID,
			Tenant:       cfg.TenantID,
			Action:       AuditSyncAccepted,
			ResourceType: event.ResourceType,
			ResourceID:   event.ResourceID,
			Outcome:      string(result.State),
			Details: map[string]string{
				"canonicalVersionId": result.CanonicalVersionID,
			},
		})
		if cfg.Jobs != nil {
			if err := EnqueueScheduledPull(ctx, cfg.Jobs, cfg.NodeID, cfg.TenantID, now); err != nil {
				return fmt.Errorf("enqueue scheduled pull: %w", err)
			}
		}
	case AckRejected:
		appendAudit(ctx, cfg.Audit, audit.SyncEvent{
			ID:           uuid.NewString(),
			Timestamp:    now,
			Actor:        cfg.NodeID,
			Tenant:       cfg.TenantID,
			Action:       AuditSyncRejected,
			ResourceType: event.ResourceType,
			ResourceID:   event.ResourceID,
			Outcome:      string(result.State),
			Details: map[string]string{
				"reason": result.RejectionReason,
			},
		})
	case AckConflicted:
		if err := p.recordConflict(ctx, cfg, event, result, now); err != nil {
			return err
		}
	case AckNeedsRetry:
		if cfg.Jobs != nil {
			payload, err := json.Marshal(RetryPushJobPayload{
				NodeID:   cfg.NodeID,
				TenantID: cfg.TenantID,
				EventID:  event.EventID,
				Reason:   "hub requested retry",
			})
			if err != nil {
				return fmt.Errorf("marshal retry push job payload: %w", err)
			}
			if err := cfg.Jobs.Enqueue(ctx, store.JobRecord{
				ID:        uuid.NewString(),
				Type:      JobTypeRetryPush,
				Payload:   payload,
				Status:    store.JobStatusPending,
				CreatedAt: now,
				UpdatedAt: now,
				RunAfter:  result.RetryAfter,
			}); err != nil {
				return fmt.Errorf("enqueue retry push job: %w", err)
			}
		}
	}
	return nil
}

func (p *Pusher) recordConflict(ctx context.Context, cfg Config, event LocalEvent, result PushResult, now time.Time) error {
	conflictID := uuid.NewString()
	if cfg.Conflicts != nil {
		if err := cfg.Conflicts.Append(ctx, store.ConflictRecord{
			ID:              conflictID,
			ResourceType:    event.ResourceType,
			ResourceID:      event.ResourceID,
			LocalVersionID:  event.LocalVersion,
			RemoteVersionID: result.ConflictRemoteVersionID,
			Reason:          result.ConflictReason,
			CreatedAt:       now,
		}); err != nil {
			return err
		}
	}
	if cfg.Jobs != nil {
		localEventJSON, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal conflict local event: %w", err)
		}
		payload, err := json.Marshal(ConflictJobPayload{
			NodeID:          cfg.NodeID,
			TenantID:        cfg.TenantID,
			ConflictID:      conflictID,
			EventID:         event.EventID,
			ResourceType:    event.ResourceType,
			ResourceID:      event.ResourceID,
			LocalVersionID:  event.LocalVersion,
			RemoteVersionID: result.ConflictRemoteVersionID,
			Reason:          result.ConflictReason,
			LocalEvent:      localEventJSON,
		})
		if err != nil {
			return fmt.Errorf("marshal conflict job payload: %w", err)
		}
		if err := cfg.Jobs.Enqueue(ctx, store.JobRecord{
			ID:        uuid.NewString(),
			Type:      JobTypeConflictProcessing,
			Payload:   payload,
			Status:    store.JobStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("enqueue conflict job: %w", err)
		}
	}
	appendAudit(ctx, cfg.Audit, audit.SyncEvent{
		ID:           uuid.NewString(),
		Timestamp:    now,
		Actor:        cfg.NodeID,
		Tenant:       cfg.TenantID,
		Action:       AuditSyncConflicted,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Outcome:      string(AckConflicted),
		Details: map[string]string{
			"reason": result.ConflictReason,
		},
	})
	return nil
}

// orderPushBatch sorts events so creates/updates precede deletes for the same resource.
func orderPushBatch(events []LocalEvent) []LocalEvent {
	if len(events) < 2 {
		return events
	}
	out := make([]LocalEvent, len(events))
	copy(out, events)
	// Stable partition: non-deletes first, deletes last.
	var head, tail int
	buffer := make([]LocalEvent, len(out))
	for _, event := range out {
		if event.Operation == EventTypeResourceDeleted {
			buffer[len(out)-1-tail] = event
			tail++
		} else {
			buffer[head] = event
			head++
		}
	}
	return buffer
}

func readCursorPosition(ctx context.Context, cursors store.CursorStore, name string) (int64, error) {
	if cursors == nil {
		return 0, nil
	}
	cursor, err := cursors.GetCursor(ctx, name)
	if err != nil {
		return 0, err
	}
	if cursor == nil || cursor.Position == "" {
		return 0, nil
	}
	pos, err := strconv.ParseInt(cursor.Position, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cursor %q position %q: %w", name, cursor.Position, err)
	}
	return pos, nil
}

func upsertCursor(ctx context.Context, cursors store.CursorStore, name string, position int64, now time.Time) error {
	if cursors == nil {
		return nil
	}
	return cursors.UpsertCursor(ctx, store.Cursor{
		Name:      name,
		Position:  strconv.FormatInt(position, 10),
		UpdatedAt: now,
	})
}
