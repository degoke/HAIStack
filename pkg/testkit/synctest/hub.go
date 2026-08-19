package synctest

import (
	"context"
	"fmt"
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
	tenantID        string
	processed       map[string]hasync.PushResult
	canonical       []hasync.CanonicalEvent
	nextSeq         int64
	pushErr         error
	resources       map[string]*types.ResourceEnvelope
	staleOnMismatch bool
	clock           hasync.Clock
}

// NewMemHub returns an empty in-memory sync hub. An optional tenant ID enables
// the same tenant-scope validation used by the production hub.
func NewMemHub(tenantIDs ...string) *MemHub {
	hub := &MemHub{
		processed: make(map[string]hasync.PushResult),
		resources: make(map[string]*types.ResourceEnvelope),
	}
	if len(tenantIDs) > 0 {
		hub.tenantID = tenantIDs[0]
	}
	return hub
}

// SetPushError configures the hub to return an error on the next Push call.
func (h *MemHub) SetPushError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pushErr = err
}

// SetClock configures the timestamp source used for accepted canonical versions.
func (h *MemHub) SetClock(clock hasync.Clock) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clock = clock
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
	if res == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resources[storetest.ResourceKey(res.ResourceType, res.ID)] = cloneEnvelope(res)
}

// CanonicalEvents returns a copy of the hub canonical event log.
func (h *MemHub) CanonicalEvents() []hasync.CanonicalEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]hasync.CanonicalEvent, len(h.canonical))
	for i, event := range h.canonical {
		out[i] = cloneCanonicalEvent(event)
	}
	return out
}

// Resources returns hub-side resource snapshots keyed by ResourceType/id.
func (h *MemHub) Resources() map[string]*types.ResourceEnvelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]*types.ResourceEnvelope, len(h.resources))
	for k, v := range h.resources {
		out[k] = cloneEnvelope(v)
	}
	return out
}

func (h *MemHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pushErr != nil {
		err := h.pushErr
		h.pushErr = nil
		return nil, err
	}

	results := make([]hasync.PushResult, 0, len(events))
	for _, event := range events {
		if h.tenantID != "" && event.TenantID != h.tenantID {
			return nil, fmt.Errorf("sync event tenant does not match hub tenant")
		}
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

		res, err := h.acceptedEnvelope(event.ResourceAfter)
		if err != nil {
			results = append(results, hasync.PushResult{
				EventID:         event.EventID,
				State:           hasync.AckRejected,
				RejectionReason: err.Error(),
			})
			continue
		}
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
			ResourceAfter:      cloneEnvelope(&res),
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
		if h.tenantID != "" && event.TenantID != h.tenantID {
			continue
		}
		out = append(out, cloneCanonicalEvent(event))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (h *MemHub) acceptedEnvelope(input *types.ResourceEnvelope) (types.ResourceEnvelope, error) {
	res := *cloneEnvelope(input)
	res.VersionID = uuid.NewString()
	if res.LastUpdated.IsZero() {
		if h.clock != nil {
			res.LastUpdated = h.clock()
		} else {
			res.LastUpdated = hasync.DefaultClock()
		}
	}
	meta, err := types.GetMeta(res.JSON)
	if err != nil {
		return types.ResourceEnvelope{}, err
	}
	meta.VersionID = res.VersionID
	meta.LastUpdated = res.LastUpdated
	res.JSON, err = types.SetMeta(res.JSON, *meta)
	if err != nil {
		return types.ResourceEnvelope{}, err
	}
	res.Hash, err = types.HashResource(res.JSON)
	if err != nil {
		return types.ResourceEnvelope{}, err
	}
	return res, nil
}

func cloneEnvelope(res *types.ResourceEnvelope) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	copy := *res
	copy.JSON = append([]byte(nil), res.JSON...)
	return &copy
}

func cloneCanonicalEvent(event hasync.CanonicalEvent) hasync.CanonicalEvent {
	copy := event
	copy.ResourceAfter = cloneEnvelope(event.ResourceAfter)
	return copy
}

// FixedClock returns a deterministic sync.Clock pinned to t.
func FixedClock(t time.Time) hasync.Clock {
	return func() time.Time { return t }
}
