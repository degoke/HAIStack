package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

// PostgresHub implements Hub and HubServer against a canonical postgres tenant database.
type PostgresHub struct {
	Tenant *postgres.TenantDB
	Clock  Clock
}

// Push accepts proposed local events, applies accepted writes, and returns per-event acks.
func (h *PostgresHub) Push(ctx context.Context, events []LocalEvent) ([]PushResult, error) {
	if h == nil || h.Tenant == nil {
		return nil, fmt.Errorf("postgres hub tenant is required")
	}
	clock := h.clock()

	results := make([]PushResult, 0, len(events))
	inbox := h.Tenant.InboxStore()
	resources := h.Tenant.ResourceStore()

	for _, event := range events {
		result, err := h.pushOne(ctx, event, inbox, resources, clock)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (h *PostgresHub) pushOne(
	ctx context.Context,
	event LocalEvent,
	inbox *postgres.InboxStore,
	resources *postgres.ResourceStore,
	now time.Time,
) (PushResult, error) {
	if inbox != nil {
		applied, err := inbox.IsApplied(ctx, event.EventID)
		if err != nil {
			return PushResult{}, err
		}
		if applied {
			return h.alreadyProcessedResult(ctx, event)
		}
	}

	exists, err := resources.Exists(ctx, event.ResourceType, event.ResourceID)
	if err != nil {
		return PushResult{}, err
	}
	var current *types.ResourceEnvelope
	if exists {
		current, err = resources.Read(ctx, event.ResourceType, event.ResourceID)
		if err != nil {
			return PushResult{}, err
		}
	}

	if stale, remoteVersion, reason := isStaleBase(event, current); stale {
		result := PushResult{
			EventID:                 event.EventID,
			State:                   AckConflicted,
			ConflictReason:          reason,
			ConflictRemoteVersionID: remoteVersion,
		}
		if err := h.recordPushConflict(ctx, event, result, now); err != nil {
			return PushResult{}, err
		}
		return result, nil
	}

	write, rejectReason, ok := buildHubWrite(event, now)
	if !ok {
		result := PushResult{
			EventID:         event.EventID,
			State:           AckRejected,
			RejectionReason: rejectReason,
		}
		if err := h.recordRejectedPush(ctx, event, result, now); err != nil {
			return PushResult{}, err
		}
		if inbox != nil {
			_ = inbox.MarkApplied(ctx, event.EventID, now)
		}
		return result, nil
	}

	writeResult, err := h.Tenant.ApplyWrite(ctx, write)
	if err != nil {
		return PushResult{
			EventID:    event.EventID,
			State:      AckNeedsRetry,
			RetryAfter: now.Add(30 * time.Second),
		}, nil
	}

	if inbox != nil {
		if err := inbox.MarkApplied(ctx, event.EventID, now); err != nil {
			return PushResult{}, err
		}
	}

	return PushResult{
		EventID:            event.EventID,
		State:              AckAccepted,
		CanonicalSequence:  writeResult.Event.Sequence,
		CanonicalVersionID: writeResult.Event.VersionID,
	}, nil
}

func (h *PostgresHub) alreadyProcessedResult(ctx context.Context, event LocalEvent) (PushResult, error) {
	result := PushResult{EventID: event.EventID, State: AckAlreadyProcessed}
	latest, err := h.Tenant.EventStore().LatestForResource(ctx, event.ResourceType, event.ResourceID)
	if err != nil {
		return result, err
	}
	if latest != nil {
		result.CanonicalSequence = latest.Sequence
		result.CanonicalVersionID = latest.VersionID
	}
	return result, nil
}

func (h *PostgresHub) recordPushConflict(ctx context.Context, event LocalEvent, result PushResult, now time.Time) error {
	conflictID := uuid.NewString()
	if conflicts := h.Tenant.ConflictStore(); conflicts != nil {
		if err := conflicts.Append(ctx, store.ConflictRecord{
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
	if jobs := h.Tenant.JobStore(); jobs != nil {
		payload, _ := json.Marshal(ConflictJobPayload{
			NodeID:          event.OriginNodeID,
			TenantID:        event.TenantID,
			ConflictID:      conflictID,
			EventID:         event.EventID,
			ResourceType:    event.ResourceType,
			ResourceID:      event.ResourceID,
			LocalVersionID:  event.LocalVersion,
			RemoteVersionID: result.ConflictRemoteVersionID,
			Reason:          result.ConflictReason,
		})
		_ = jobs.Enqueue(ctx, store.JobRecord{
			ID:        uuid.NewString(),
			Type:      JobTypeConflictProcessing,
			Payload:   payload,
			Status:    store.JobStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if inbox := h.Tenant.InboxStore(); inbox != nil {
		_ = inbox.MarkApplied(ctx, event.EventID, now)
	}
	return h.recordRejectedPush(ctx, event, result, now)
}

func (h *PostgresHub) recordRejectedPush(ctx context.Context, event LocalEvent, result PushResult, now time.Time) error {
	audit := h.Tenant.AuditStore()
	if audit == nil {
		return nil
	}
	action := AuditSyncRejected
	outcome := string(result.State)
	if result.State == AckConflicted {
		action = AuditSyncConflicted
	}
	return audit.Append(ctx, store.AuditRecord{
		ID:           event.EventID,
		Timestamp:    now,
		Actor:        event.OriginNodeID,
		Action:       action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Outcome:      outcome,
		Details: map[string]string{
			"reason": firstNonEmpty(result.RejectionReason, result.ConflictReason),
		},
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func isStaleBase(event LocalEvent, current *types.ResourceEnvelope) (bool, string, string) {
	switch event.Operation {
	case EventTypeResourceCreated:
		if current != nil {
			return true, current.VersionID, "resource already exists"
		}
		return false, "", ""
	case EventTypeResourceUpdated, EventTypeResourceDeleted:
		if current == nil {
			return true, "", "resource not found"
		}
		if event.BaseCloudVersion != "" && event.BaseCloudVersion != current.VersionID {
			return true, current.VersionID, "stale base version"
		}
		return false, current.VersionID, ""
	default:
		return true, "", fmt.Sprintf("unsupported operation %q", event.Operation)
	}
}

func buildHubWrite(event LocalEvent, now time.Time) (postgres.Write, string, bool) {
	switch event.Operation {
	case EventTypeResourceCreated, EventTypeResourceUpdated:
		if event.ResourceAfter == nil {
			return postgres.Write{}, "missing resource payload", false
		}
		res := *event.ResourceAfter
		action := store.VersionActionUpdate
		if event.Operation == EventTypeResourceCreated {
			action = store.VersionActionCreate
		}
		return postgres.Write{
			Resource: &res,
			Action:   action,
			Audit: store.AuditRecord{
				Timestamp: now,
				Actor:     event.OriginNodeID,
				Action:    AuditDevicePushed,
			},
		}, "", true

	case EventTypeResourceDeleted:
		res := event.ResourceAfter
		if res == nil {
			res = &types.ResourceEnvelope{
				ResourceType: event.ResourceType,
				ID:           event.ResourceID,
				Hash:         event.ResourceHash,
			}
		}
		return postgres.Write{
			Resource: res,
			Action:   store.VersionActionDelete,
			Audit: store.AuditRecord{
				Timestamp: now,
				Actor:     event.OriginNodeID,
				Action:    AuditDevicePushed,
			},
		}, "", true

	default:
		return postgres.Write{}, fmt.Sprintf("unsupported operation %q", event.Operation), false
	}
}

// Pull returns accepted canonical events after a sequence checkpoint.
func (h *PostgresHub) Pull(ctx context.Context, afterSequence int64, limit int) ([]CanonicalEvent, error) {
	if h == nil || h.Tenant == nil {
		return nil, fmt.Errorf("postgres hub tenant is required")
	}

	events, err := h.Tenant.EventStore().ReadSince(ctx, afterSequence, limit)
	if err != nil {
		return nil, err
	}

	resources := h.Tenant.ResourceStore()
	clock := h.clock()

	out := make([]CanonicalEvent, 0, len(events))
	for _, event := range events {
		canonical, err := resourceEventToCanonical(ctx, event, h.tenantID(), resources, clock)
		if err != nil {
			return nil, err
		}
		out = append(out, canonical)
	}
	return out, nil
}

func resourceEventToCanonical(
	ctx context.Context,
	event store.ResourceEvent,
	tenantID string,
	resources *postgres.ResourceStore,
	now time.Time,
) (CanonicalEvent, error) {
	op, err := eventTypeForAction(event.Action)
	if err != nil {
		return CanonicalEvent{}, err
	}

	var resourceAfter *types.ResourceEnvelope
	if event.Action != store.EventActionDelete {
		res, readErr := resources.Read(ctx, event.ResourceType, event.ID)
		if readErr == nil {
			resourceAfter = res
		}
	}

	return CanonicalEvent{
		EventID:            CanonicalEventID(event.Sequence),
		TenantID:           tenantID,
		ResourceType:       event.ResourceType,
		ResourceID:         event.ID,
		Operation:          op,
		LocalVersion:       event.VersionID,
		ResourceAfter:      resourceAfter,
		ResourceHash:       event.Hash,
		CanonicalSequence:  event.Sequence,
		CanonicalVersionID: event.VersionID,
		Status:             CanonicalStatusAccepted,
		AcknowledgedAt:     now,
	}, nil
}

func (h *PostgresHub) clock() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return DefaultClock()
}

func (h *PostgresHub) tenantID() string {
	if h.Tenant != nil {
		return h.Tenant.TenantID()
	}
	return ""
}
