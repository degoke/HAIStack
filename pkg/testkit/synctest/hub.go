package synctest

import (
	"context"
	"sync"
	"time"

	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

var _ hasync.Hub = (*MemHub)(nil)

// MemHub is an in-process fake hub implementing sync.Hub.
type MemHub struct {
	mu              sync.Mutex
	processed       map[string]hasync.PushResult
	canonical       []hasync.CanonicalEvent
	nextSeq         int64
	pushErr         error
	resources       map[string]*types.ResourceEnvelope
	staleOnMismatch bool
}

// NewMemHub returns an empty in-memory sync hub.
func NewMemHub() *MemHub {
	return &MemHub{
		processed: make(map[string]hasync.PushResult),
		resources: make(map[string]*types.ResourceEnvelope),
	}
}

// SetPushError configures the hub to return an error on the next Push call.
func (h *MemHub) SetPushError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pushErr = err
}

// SetStaleOnMismatch enables stale-base conflict detection when BaseCloudVersion
// does not match the current hub resource version.
func (h *MemHub) SetStaleOnMismatch(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.staleOnMismatch = enabled
}

// SeedResource pre-populates a resource in the hub canonical state.
func (h *MemHub) SeedResource(res *types.ResourceEnvelope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resources[storetest.ResourceKey(res.ResourceType, res.ID)] = res
}

// CanonicalEvents returns a copy of the hub canonical event log.
func (h *MemHub) CanonicalEvents() []hasync.CanonicalEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]hasync.CanonicalEvent, len(h.canonical))
	copy(out, h.canonical)
	return out
}

// Resources returns hub-side resource snapshots keyed by ResourceType/id.
func (h *MemHub) Resources() map[string]*types.ResourceEnvelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]*types.ResourceEnvelope, len(h.resources))
	for k, v := range h.resources {
		copy := *v
		out[k] = &copy
	}
	return out
}

func (h *MemHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pushErr != nil {
		return nil, h.pushErr
	}

	results := make([]hasync.PushResult, 0, len(events))
	for _, event := range events {
		if prior, ok := h.processed[event.EventID]; ok {
			results = append(results, hasync.PushResult{
				EventID:            event.EventID,
				State:              hasync.AckAlreadyProcessed,
				CanonicalSequence:  prior.CanonicalSequence,
				CanonicalVersionID: prior.CanonicalVersionID,
			})
			continue
		}

		key := storetest.ResourceKey(event.ResourceType, event.ResourceID)
		current := h.resources[key]

		if event.Operation == hasync.EventTypeResourceCreated && current != nil {
			results = append(results, hasync.PushResult{
				EventID:                 event.EventID,
				State:                   hasync.AckConflicted,
				ConflictReason:          "resource already exists",
				ConflictRemoteVersionID: current.VersionID,
			})
			continue
		}

		if event.Operation != hasync.EventTypeResourceCreated {
			if current == nil {
				results = append(results, hasync.PushResult{
					EventID:        event.EventID,
					State:          hasync.AckConflicted,
					ConflictReason: "resource not found",
				})
				continue
			}
			if h.staleOnMismatch && event.BaseCloudVersion != "" && event.BaseCloudVersion != current.VersionID {
				results = append(results, hasync.PushResult{
					EventID:                 event.EventID,
					State:                   hasync.AckConflicted,
					ConflictReason:          "stale base version",
					ConflictRemoteVersionID: current.VersionID,
				})
				continue
			}
		}

		if event.Operation == hasync.EventTypeResourceDeleted {
			delete(h.resources, key)
			h.nextSeq++
			versionID := uuid.NewString()
			result := hasync.PushResult{
				EventID:            event.EventID,
				State:              hasync.AckAccepted,
				CanonicalSequence:  h.nextSeq,
				CanonicalVersionID: versionID,
			}
			h.processed[event.EventID] = result
			h.canonical = append(h.canonical, hasync.CanonicalEvent{
				TenantID:           event.TenantID,
				ResourceType:       event.ResourceType,
				ResourceID:         event.ResourceID,
				Operation:          event.Operation,
				ResourceHash:       event.ResourceHash,
				CanonicalSequence:  h.nextSeq,
				CanonicalVersionID: versionID,
				Status:             hasync.CanonicalStatusAccepted,
			})
			results = append(results, result)
			continue
		}

		if event.ResourceAfter == nil {
			results = append(results, hasync.PushResult{
				EventID:         event.EventID,
				State:           hasync.AckRejected,
				RejectionReason: "missing resource payload",
			})
			continue
		}

		res := *event.ResourceAfter
		res.VersionID = uuid.NewString()
		h.resources[key] = &res
		h.nextSeq++
		result := hasync.PushResult{
			EventID:            event.EventID,
			State:              hasync.AckAccepted,
			CanonicalSequence:  h.nextSeq,
			CanonicalVersionID: res.VersionID,
		}
		h.processed[event.EventID] = result
		h.canonical = append(h.canonical, hasync.CanonicalEvent{
			TenantID:           event.TenantID,
			ResourceType:       event.ResourceType,
			ResourceID:         event.ResourceID,
			Operation:          event.Operation,
			ResourceAfter:      &res,
			ResourceHash:       res.Hash,
			CanonicalSequence:  h.nextSeq,
			CanonicalVersionID: res.VersionID,
			Status:             hasync.CanonicalStatusAccepted,
		})
		results = append(results, result)
	}
	return results, nil
}

func (h *MemHub) Pull(_ context.Context, afterSequence int64, limit int) ([]hasync.CanonicalEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []hasync.CanonicalEvent
	for _, event := range h.canonical {
		if event.CanonicalSequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// FixedClock returns a deterministic sync.Clock pinned to t.
func FixedClock(t time.Time) hasync.Clock {
	return func() time.Time { return t }
}
