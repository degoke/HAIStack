package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/google/uuid"
)

// PostgresHub implements Hub and HubServer against a canonical postgres tenant database.
type PostgresHub struct {
	Tenant        *postgres.TenantDB
	Clock         Clock
	SearchIndexer SearchIndexer
	Validator     validate.Validator
}

// PushFor validates the HTTP caller's node and tenant binding before using
// the tenant-scoped hub implementation.
func (h *PostgresHub) PushFor(ctx context.Context, nodeID, tenantID string, events []LocalEvent) ([]PushResult, error) {
	if err := h.validateScope(ctx, nodeID, tenantID); err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.TenantID != tenantID || event.OriginNodeID != nodeID {
			return nil, fmt.Errorf("sync event tenant or origin node does not match request scope")
		}
	}
	return h.Push(ctx, events)
}

// PullFor validates the HTTP caller's tenant binding and verifies that the
// tenant-scoped store cannot return cross-tenant events.
func (h *PostgresHub) PullFor(ctx context.Context, nodeID, tenantID string, afterSequence int64, limit int) ([]CanonicalEvent, error) {
	if err := h.validateScope(ctx, nodeID, tenantID); err != nil {
		return nil, err
	}
	events, err := h.Pull(ctx, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.TenantID != tenantID {
			return nil, fmt.Errorf("sync hub returned an event outside request tenant scope")
		}
	}
	return events, nil
}

func (h *PostgresHub) validateScope(ctx context.Context, nodeID, tenantID string) error {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("nodeId and tenantId are required")
	}
	if h == nil || h.Tenant == nil || h.Tenant.TenantID() != tenantID {
		return fmt.Errorf("sync request tenant is not bound to this hub")
	}
	if _, err := h.Tenant.NodeRegistry().Get(ctx, nodeID); err != nil {
		return fmt.Errorf("sync node is not registered for tenant %q: %w", tenantID, err)
	}
	return nil
}

// Push accepts proposed local events, applies accepted writes, and returns per-event acks.
func (h *PostgresHub) Push(ctx context.Context, events []LocalEvent) ([]PushResult, error) {
	if h == nil || h.Tenant == nil {
		return nil, fmt.Errorf("postgres hub tenant is required")
	}
	clock := h.clock()
	validator, err := h.validator()
	if err != nil {
		return nil, err
	}

	results := make([]PushResult, 0, len(events))

	for _, event := range events {
		result, err := h.pushOne(ctx, event, clock, validator)
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
	now time.Time,
	validator validate.Validator,
) (PushResult, error) {
	if strings.TrimSpace(event.EventID) == "" {
		return PushResult{}, fmt.Errorf("sync event id is required")
	}
	if event.ResourceType == "" || event.ResourceID == "" {
		return PushResult{}, fmt.Errorf("sync event resource type and id are required")
	}
	if event.TenantID != "" && event.TenantID != h.tenantID() {
		return PushResult{}, fmt.Errorf("sync event tenant does not match hub tenant")
	}
	session, err := h.Tenant.BeginSession(ctx)
	if err != nil {
		return PushResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()

	inbox := session.InboxStore().(*postgres.InboxStore)
	claimed, err := inbox.ClaimPush(ctx, event.EventID, now)
	if err != nil {
		return PushResult{}, err
	}
	if !claimed {
		if payload, applied, err := inbox.GetAckPayload(ctx, event.EventID); err != nil {
			return PushResult{}, err
		} else if applied {
			if len(payload) > 0 {
				var original PushResult
				if err := json.Unmarshal(payload, &original); err != nil {
					return PushResult{}, fmt.Errorf("decode stored push acknowledgement: %w", err)
				}
				return replayedPushResult(event.EventID, original), nil
			}
			return h.alreadyProcessedResultInSession(ctx, session, event)
		}
		return PushResult{}, fmt.Errorf("push inbox claim lost without an existing event record")
	}

	resources := session.ResourceStore().(*postgres.ResourceStore)
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
		if err := h.persistTerminalInSession(ctx, session, event, result, now); err != nil {
			return PushResult{}, err
		}
		if err := session.Commit(ctx); err != nil {
			return PushResult{}, err
		}
		committed = true
		return result, nil
	}

	write, rejectReason, ok := buildHubWrite(ctx, event, now, validator)
	if !ok {
		result := PushResult{
			EventID:         event.EventID,
			State:           AckRejected,
			RejectionReason: rejectReason,
		}
		if err := h.persistTerminalInSession(ctx, session, event, result, now); err != nil {
			return PushResult{}, err
		}
		if err := session.Commit(ctx); err != nil {
			return PushResult{}, err
		}
		committed = true
		return result, nil
	}

	write.ExpectedVersion = ""
	if current != nil {
		write.ExpectedVersion = current.VersionID
	}
	write.EventID = event.EventID
	write.OriginNodeID = event.OriginNodeID
	write.LocalVersionID = event.LocalVersion
	if h.SearchIndexer != nil && write.Resource != nil {
		entries, indexErr := h.SearchIndexer.BuildSearchEntries(ctx, write.Resource)
		if indexErr != nil {
			return PushResult{}, indexErr
		}
		write.SearchEntries = entries
		if len(entries) == 0 {
			if err := session.SearchStore().RemoveIndex(ctx, write.Resource.ResourceType, write.Resource.ID); err != nil {
				return PushResult{}, err
			}
		}
	}

	writeResult, err := session.ApplyWrite(ctx, write)
	if err != nil {
		if errors.Is(err, postgres.ErrVersionMismatch) {
			latest, readErr := resources.Read(ctx, event.ResourceType, event.ResourceID)
			if readErr != nil {
				// A concurrent delete can legitimately make the resource
				// disappear after the conditional write fails. Treat that as a
				// conflict with no remote version rather than leaking an
				// unacknowledged push error.
				exists, existsErr := resources.Exists(ctx, event.ResourceType, event.ResourceID)
				if existsErr != nil {
					return PushResult{}, existsErr
				}
				if exists {
					return PushResult{}, readErr
				}
				latest = nil
			}
			result := PushResult{
				EventID:        event.EventID,
				State:          AckConflicted,
				ConflictReason: "stale base version",
			}
			if latest != nil {
				result.ConflictRemoteVersionID = latest.VersionID
			}
			if err := h.persistTerminalInSession(ctx, session, event, result, now); err != nil {
				return PushResult{}, err
			}
			if err := session.Commit(ctx); err != nil {
				return PushResult{}, err
			}
			committed = true
			return result, nil
		}
		result := classifyApplyWriteError(event, err, now)
		if result.State != AckNeedsRetry {
			_ = session.Rollback(ctx)
			committed = true
			return h.persistTerminalResult(ctx, event, result, now)
		}
		return PushResult{
			EventID:    event.EventID,
			State:      AckNeedsRetry,
			RetryAfter: now.Add(30 * time.Second),
		}, nil
	}

	result := PushResult{
		EventID:            event.EventID,
		State:              AckAccepted,
		CanonicalSequence:  writeResult.Event.Sequence,
		CanonicalVersionID: writeResult.Event.VersionID,
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return PushResult{}, err
	}
	if err := inbox.MarkAppliedWithPayload(ctx, event.EventID, payload, now); err != nil {
		return PushResult{}, err
	}
	if err := session.Commit(ctx); err != nil {
		return PushResult{}, err
	}
	committed = true
	return result, nil
}

func replayedPushResult(eventID string, original PushResult) PushResult {
	original.EventID = eventID
	original.State = AckAlreadyProcessed
	original.RetryAfter = time.Time{}
	return original
}

func (h *PostgresHub) alreadyProcessedResultInSession(ctx context.Context, session *postgres.Session, event LocalEvent) (PushResult, error) {
	result := PushResult{EventID: event.EventID, State: AckAlreadyProcessed}
	latest, err := session.EventStore().LatestForResource(ctx, event.ResourceType, event.ResourceID)
	if err != nil {
		return result, err
	}
	if latest != nil {
		result.CanonicalSequence = latest.Sequence
		result.CanonicalVersionID = latest.VersionID
	}
	return result, nil
}

func (h *PostgresHub) persistTerminalResult(ctx context.Context, event LocalEvent, result PushResult, now time.Time) (PushResult, error) {
	session, err := h.Tenant.BeginSession(ctx)
	if err != nil {
		return PushResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()
	if err := h.persistTerminalInSession(ctx, session, event, result, now); err != nil {
		return PushResult{}, err
	}
	if err := session.Commit(ctx); err != nil {
		return PushResult{}, err
	}
	committed = true
	return result, nil
}

func (h *PostgresHub) persistTerminalInSession(ctx context.Context, session *postgres.Session, event LocalEvent, result PushResult, now time.Time) error {
	resource := resourceForEvent(event)
	action := AuditSyncRejected
	outcome := postgres.WriteOutcomeRejected
	if result.State == AckConflicted {
		action = AuditSyncConflicted
		outcome = postgres.WriteOutcomeConflicted
	}
	writeResult, err := session.ApplyWrite(ctx, postgres.Write{
		Resource:                resource,
		Outcome:                 outcome,
		RejectionReason:         firstNonEmpty(result.RejectionReason, result.ConflictReason),
		ConflictLocalVersionID:  event.LocalVersion,
		ConflictRemoteVersionID: result.ConflictRemoteVersionID,
		Audit: store.AuditRecord{
			ID:           event.EventID,
			Timestamp:    now,
			Actor:        event.OriginNodeID,
			Action:       action,
			ResourceType: event.ResourceType,
			ResourceID:   event.ResourceID,
			Outcome:      string(result.State),
		},
	})
	if err != nil {
		return err
	}
	if result.State == AckConflicted && writeResult.ConflictID != "" {
		jobs := session.JobStore()
		if jobs != nil {
			localEventJSON, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("marshal conflict local event: %w", err)
			}
			tenantID := event.TenantID
			if tenantID == "" {
				tenantID = h.tenantID()
			}
			payload, err := json.Marshal(ConflictJobPayload{
				NodeID:          event.OriginNodeID,
				TenantID:        tenantID,
				ConflictID:      writeResult.ConflictID,
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
			if err := jobs.Enqueue(ctx, store.JobRecord{
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
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal push acknowledgement: %w", err)
	}
	return session.InboxStore().(*postgres.InboxStore).MarkAppliedWithPayload(ctx, event.EventID, payload, now)
}

func resourceForEvent(event LocalEvent) *types.ResourceEnvelope {
	if event.ResourceAfter != nil {
		copy := *event.ResourceAfter
		return &copy
	}
	return &types.ResourceEnvelope{
		ResourceType: event.ResourceType,
		ID:           event.ResourceID,
		Hash:         event.ResourceHash,
	}
}

func classifyApplyWriteError(event LocalEvent, err error, now time.Time) PushResult {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "resource already exists"):
		return PushResult{
			EventID:                 event.EventID,
			State:                   AckConflicted,
			ConflictReason:          "resource already exists",
			ConflictRemoteVersionID: event.BaseCloudVersion,
		}
	case strings.Contains(msg, "resource not found"):
		return PushResult{
			EventID:                 event.EventID,
			State:                   AckConflicted,
			ConflictReason:          "resource not found",
			ConflictRemoteVersionID: event.BaseCloudVersion,
		}
	case strings.Contains(msg, "unsupported version action"), strings.Contains(msg, "resource envelope is nil"):
		return PushResult{
			EventID:         event.EventID,
			State:           AckRejected,
			RejectionReason: msg,
		}
	default:
		return PushResult{
			EventID:    event.EventID,
			State:      AckNeedsRetry,
			RetryAfter: now.Add(30 * time.Second),
		}
	}
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

func buildHubWrite(ctx context.Context, event LocalEvent, now time.Time, validator validate.Validator) (postgres.Write, string, bool) {
	switch event.Operation {
	case EventTypeResourceCreated, EventTypeResourceUpdated:
		if event.ResourceAfter == nil {
			return postgres.Write{}, "missing resource payload", false
		}
		if event.ResourceAfter.ResourceType != event.ResourceType || event.ResourceAfter.ID != event.ResourceID {
			return postgres.Write{}, "resource payload identity does not match event", false
		}
		parsed, err := types.NewJSONCodec().ParseJSON(event.ResourceType, event.ResourceAfter.JSON)
		if err != nil {
			return postgres.Write{}, "resource payload is not valid FHIR JSON", false
		}
		if parsed.ID != event.ResourceID {
			return postgres.Write{}, "resource payload JSON identity does not match event", false
		}
		res := *event.ResourceAfter
		if validator != nil {
			if err := validator.ValidateResource(ctx, &res); err != nil {
				return postgres.Write{}, "resource payload failed FHIR validation: " + err.Error(), false
			}
		}
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
		if res.ResourceType != event.ResourceType || res.ID != event.ResourceID {
			return postgres.Write{}, "resource payload identity does not match event", false
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

	history := h.Tenant.HistoryStore()
	clock := h.clock()

	out := make([]CanonicalEvent, 0, len(events))
	for _, event := range events {
		canonical, err := resourceEventToCanonical(ctx, event, h.tenantID(), history, clock)
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
	history *postgres.HistoryStore,
	now time.Time,
) (CanonicalEvent, error) {
	op, err := eventTypeForAction(event.Action)
	if err != nil {
		return CanonicalEvent{}, err
	}

	var resourceAfter *types.ResourceEnvelope
	if event.Action != store.EventActionDelete {
		if history == nil {
			return CanonicalEvent{}, fmt.Errorf("history store required for canonical event %d", event.Sequence)
		}
		versions, historyErr := history.GetHistory(ctx, event.ResourceType, event.ID)
		if historyErr != nil {
			return CanonicalEvent{}, fmt.Errorf("read canonical history for event %d: %w", event.Sequence, historyErr)
		}
		for _, version := range versions {
			if version.VersionID == event.VersionID && version.Resource != nil {
				copy := *version.Resource
				resourceAfter = &copy
				break
			}
		}
		if resourceAfter == nil {
			return CanonicalEvent{}, fmt.Errorf("canonical event %d missing historical resource version %q", event.Sequence, event.VersionID)
		}
	}

	return CanonicalEvent{
		EventID:            CanonicalEventID(tenantID, event.Sequence),
		OriginNodeID:       event.OriginNodeID,
		TenantID:           tenantID,
		ResourceType:       event.ResourceType,
		ResourceID:         event.ID,
		Operation:          op,
		LocalVersion:       firstNonEmpty(event.LocalVersion, event.VersionID),
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

func (h *PostgresHub) validator() (validate.Validator, error) {
	if h.Validator != nil {
		return h.Validator, nil
	}
	engine, err := validate.NewEngine(validate.Config{})
	if err != nil {
		return nil, fmt.Errorf("initialize hub validator: %w", err)
	}
	return validate.NewCoreValidator(engine, validate.ValidateOptions{RequireID: true}), nil
}

func (h *PostgresHub) tenantID() string {
	if h.Tenant != nil {
		return h.Tenant.TenantID()
	}
	return ""
}
