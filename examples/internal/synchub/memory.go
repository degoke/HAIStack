package synchub

import (
	"context"
	"fmt"
	"sync"
	"time"

	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// MemoryHub is a tiny in-process sync hub for examples.
type MemoryHub struct {
	mu      sync.Mutex
	now     func() time.Time
	nextSeq int64
	events  []hasync.CanonicalEvent
}

func NewMemoryHub() *MemoryHub {
	return &MemoryHub{
		now: time.Now().UTC,
	}
}

func (h *MemoryHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	results := make([]hasync.PushResult, 0, len(events))
	for _, event := range events {
		h.nextSeq++
		canonicalVersion := fmt.Sprintf("hub-v%d", h.nextSeq)
		canonical := hasync.CanonicalEvent{
			EventID:            hasync.CanonicalEventID(event.TenantID, h.nextSeq),
			OriginNodeID:       event.OriginNodeID,
			TenantID:           event.TenantID,
			ResourceType:       event.ResourceType,
			ResourceID:         event.ResourceID,
			Operation:          event.Operation,
			BaseCloudVersion:   event.BaseCloudVersion,
			LocalVersion:       event.LocalVersion,
			ResourceAfter:      cloneResource(event.ResourceAfter, canonicalVersion, h.now()),
			ResourceHash:       event.ResourceHash,
			CanonicalSequence:  h.nextSeq,
			CanonicalVersionID: canonicalVersion,
			Status:             hasync.CanonicalStatusAccepted,
			AcknowledgedAt:     h.now(),
		}
		h.events = append(h.events, canonical)
		results = append(results, hasync.PushResult{
			EventID:            event.EventID,
			State:              hasync.AckAccepted,
			CanonicalSequence:  canonical.CanonicalSequence,
			CanonicalVersionID: canonical.CanonicalVersionID,
		})
	}
	return results, nil
}

func (h *MemoryHub) Pull(_ context.Context, afterSequence int64, limit int) ([]hasync.CanonicalEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if limit <= 0 {
		limit = len(h.events)
	}
	out := make([]hasync.CanonicalEvent, 0, limit)
	for _, event := range h.events {
		if event.CanonicalSequence <= afterSequence {
			continue
		}
		out = append(out, cloneCanonicalEvent(event))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func cloneCanonicalEvent(event hasync.CanonicalEvent) hasync.CanonicalEvent {
	out := event
	out.ResourceAfter = cloneResource(event.ResourceAfter, event.CanonicalVersionID, event.AcknowledgedAt)
	return out
}

func cloneResource(res *types.ResourceEnvelope, versionID string, now time.Time) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	copy := *res
	copy.VersionID = versionID
	if copy.LastUpdated.IsZero() {
		copy.LastUpdated = now
	}
	if copy.JSON != nil {
		copy.JSON = append([]byte(nil), copy.JSON...)
	}
	return &copy
}
