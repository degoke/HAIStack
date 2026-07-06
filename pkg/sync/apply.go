package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Applier applies accepted canonical events to a local node without emitting outbox events.
type Applier struct {
	Resources store.ResourceStore
	History   store.HistoryStore
	Inbox     store.InboxStore
	Search    store.SearchStore
	Indexer   SearchIndexer
	Clock     Clock
}

// ApplyCanonical replays one accepted canonical event locally with inbox idempotency.
func (a *Applier) ApplyCanonical(ctx context.Context, event CanonicalEvent) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("applier is nil")
	}
	if event.Status != CanonicalStatusAccepted {
		return false, fmt.Errorf("cannot apply non-accepted canonical event seq %d", event.CanonicalSequence)
	}

	id := CanonicalEventID(event.CanonicalSequence)
	if a.Inbox != nil {
		applied, err := a.Inbox.IsApplied(ctx, id)
		if err != nil {
			return false, err
		}
		if applied {
			return false, nil
		}
	}

	now := time.Now().UTC()
	if a.Clock != nil {
		now = a.Clock()
	}

	if err := a.applyResource(ctx, event, now); err != nil {
		return false, err
	}

	if a.Inbox != nil {
		if err := a.Inbox.MarkApplied(ctx, id, now); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (a *Applier) applyResource(ctx context.Context, event CanonicalEvent, now time.Time) error {
	switch event.Operation {
	case EventTypeResourceCreated, EventTypeResourceUpdated:
		if event.ResourceAfter == nil {
			return fmt.Errorf("canonical event %d missing resource payload", event.CanonicalSequence)
		}
		res := *event.ResourceAfter
		res.VersionID = event.CanonicalVersionID
		if res.LastUpdated.IsZero() {
			res.LastUpdated = now
		}

		exists, err := a.Resources.Exists(ctx, res.ResourceType, res.ID)
		if err != nil {
			return err
		}
		if exists {
			if err := a.Resources.Update(ctx, &res); err != nil {
				return err
			}
		} else {
			if err := a.Resources.Create(ctx, &res); err != nil {
				return err
			}
		}

		action := store.VersionActionUpdate
		if event.Operation == EventTypeResourceCreated {
			action = store.VersionActionCreate
		}
		if err := a.History.AppendVersion(ctx, store.ResourceVersion{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    event.CanonicalVersionID,
			Action:       action,
			Timestamp:    now,
			Resource:     &res,
			Hash:         res.Hash,
		}); err != nil {
			return err
		}
		return a.indexResource(ctx, &res)

	case EventTypeResourceDeleted:
		exists, err := a.Resources.Exists(ctx, event.ResourceType, event.ResourceID)
		if err != nil {
			return err
		}
		if exists {
			if err := a.Resources.Delete(ctx, event.ResourceType, event.ResourceID); err != nil {
				return err
			}
		}
		if a.Search != nil {
			if err := a.Search.RemoveIndex(ctx, event.ResourceType, event.ResourceID); err != nil {
				return err
			}
		}
		return a.History.AppendVersion(ctx, store.ResourceVersion{
			ResourceType: event.ResourceType,
			ID:           event.ResourceID,
			VersionID:    event.CanonicalVersionID,
			Action:       store.VersionActionDelete,
			Timestamp:    now,
			Hash:         event.ResourceHash,
			Deleted:      true,
		})

	default:
		return fmt.Errorf("unsupported canonical operation %q", event.Operation)
	}
}

func (a *Applier) indexResource(ctx context.Context, res *types.ResourceEnvelope) error {
	if a.Search == nil || a.Indexer == nil {
		return nil
	}
	entries, err := a.Indexer.BuildSearchEntries(ctx, res)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := a.Search.RemoveIndex(ctx, entry.ResourceType, entry.ID); err != nil {
			return err
		}
		if err := a.Search.Index(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

// ResourceEnvelopeFromCanonical builds a minimal envelope for tombstone deletes.
func ResourceEnvelopeFromCanonical(event CanonicalEvent) *types.ResourceEnvelope {
	if event.ResourceAfter != nil {
		return event.ResourceAfter
	}
	return &types.ResourceEnvelope{
		ResourceType: event.ResourceType,
		ID:           event.ResourceID,
		VersionID:    event.CanonicalVersionID,
		Hash:         event.ResourceHash,
	}
}
