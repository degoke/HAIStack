package sync

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// EnrichLocalEvent maps a store.ResourceEvent into a sync protocol LocalEvent.
func EnrichLocalEvent(
	ctx context.Context,
	event store.ResourceEvent,
	nodeID, tenantID string,
	history store.HistoryStore,
) (LocalEvent, error) {
	op, err := eventTypeForAction(event.Action)
	if err != nil {
		return LocalEvent{}, err
	}

	baseVersion, resourceAfter, err := resolveBaseAndResource(ctx, event, history)
	if err != nil {
		return LocalEvent{}, err
	}

	eventID := OutboxEventID(nodeID, tenantID, event.Sequence)
	return LocalEvent{
		EventID:          eventID,
		OriginNodeID:     nodeID,
		TenantID:         tenantID,
		ResourceType:     event.ResourceType,
		ResourceID:       event.ID,
		Operation:        op,
		BaseCloudVersion: baseVersion,
		LocalVersion:     event.VersionID,
		ResourceAfter:    resourceAfter,
		ResourceHash:     event.Hash,
		Status:           LocalEventStatusPending,
		CreatedAt:        event.Timestamp,
		OutboxSequence:   event.Sequence,
	}, nil
}

func eventTypeForAction(action store.EventAction) (EventType, error) {
	switch action {
	case store.EventActionCreate:
		return EventTypeResourceCreated, nil
	case store.EventActionUpdate:
		return EventTypeResourceUpdated, nil
	case store.EventActionDelete:
		return EventTypeResourceDeleted, nil
	default:
		return "", fmt.Errorf("unsupported event action %q", action)
	}
}

func resolveBaseAndResource(
	ctx context.Context,
	event store.ResourceEvent,
	history store.HistoryStore,
) (baseVersion string, resourceAfter *types.ResourceEnvelope, err error) {
	if history == nil {
		return "", nil, nil
	}

	versions, err := history.GetHistory(ctx, event.ResourceType, event.ID)
	if err != nil {
		return "", nil, fmt.Errorf("read history for enrichment: %w", err)
	}

	var priorVersionID string
	for i, version := range versions {
		if version.VersionID != event.VersionID {
			continue
		}
		if i > 0 {
			priorVersionID = versions[i-1].VersionID
		}
		if event.Action == store.EventActionDelete {
			return priorVersionID, nil, nil
		}
		if version.Resource != nil {
			copied := *version.Resource
			resourceAfter = &copied
		}
		return priorVersionID, resourceAfter, nil
	}

	if event.Action == store.EventActionDelete {
		return "", nil, nil
	}
	return "", nil, nil
}
