package sync_test

import (
	"context"
	"encoding/json"
	stdsync "sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/uuid"
)

type memResolutionHandler struct {
	mu    stdsync.Mutex
	calls []conflict.MergeResult
}

func (h *memResolutionHandler) OnConflictResolution(_ context.Context, _ hasync.ConflictJobPayload, result conflict.MergeResult) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, result)
	return nil
}

func TestConflictProcessingJobAutoMerge(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	resources := newMemResourceStore()
	history := newMemHistoryStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{}
	audit := &memAuditStore{}

	base := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "base-v1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
	}
	local := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "local-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","telecom":[{"system":"phone","value":"111"}]}`),
	}
	current := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "cloud-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","address":[{"city":"NYC"}]}`),
	}

	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     base,
	})
	_ = resources.Create(ctx, current)

	localEvent := hasync.LocalEvent{
		EventID:          "event-1",
		OriginNodeID:     "node-a",
		TenantID:         "tenant-a",
		ResourceType:     "Patient",
		ResourceID:       "p1",
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: base.VersionID,
		LocalVersion:     local.VersionID,
		ResourceAfter:    local,
	}
	localEventJSON, _ := json.Marshal(localEvent)
	payload, _ := json.Marshal(hasync.ConflictJobPayload{
		NodeID:          "node-a",
		TenantID:        "tenant-a",
		ConflictID:      "conflict-1",
		EventID:         localEvent.EventID,
		ResourceType:    "Patient",
		ResourceID:      "p1",
		LocalVersionID:  local.VersionID,
		RemoteVersionID: current.VersionID,
		Reason:          "stale base version",
		LocalEvent:      localEventJSON,
	})
	_ = jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      hasync.JobTypeConflictProcessing,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})

	processor := &hasync.JobProcessor{
		Engine: hasync.NewEngine(hasync.Config{
			NodeID:         "node-a",
			TenantID:       "tenant-a",
			Resources:      resources,
			History:        history,
			Conflicts:      conflicts,
			Jobs:           jobs,
			Audit:          audit,
			ConflictEngine: conflict.NewDefaultEngine(),
			Clock:          fixedClock(now),
		}),
		Jobs:  jobs,
		Clock: fixedClock(now),
	}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	found := false
	for _, rec := range audit.records {
		if rec.Action == hasync.AuditConflictAutoMerged {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-merged audit record, got %+v", audit.records)
	}
}

func TestConflictProcessingJobNeedsReview(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	resources := newMemResourceStore()
	history := newMemHistoryStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{}
	audit := &memAuditStore{}

	base := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "base-v1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","birthDate":"2000-01-01"}`),
	}
	local := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "local-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","birthDate":"2000-02-02"}`),
	}
	current := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "cloud-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","telecom":[{"system":"phone","value":"111"}]}`),
	}

	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     base,
	})
	_ = resources.Create(ctx, current)

	localEvent := hasync.LocalEvent{
		EventID:          "event-2",
		OriginNodeID:     "node-a",
		TenantID:         "tenant-a",
		ResourceType:     "Patient",
		ResourceID:       "p1",
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: base.VersionID,
		LocalVersion:     local.VersionID,
		ResourceAfter:    local,
	}
	localEventJSON, _ := json.Marshal(localEvent)
	payload, _ := json.Marshal(hasync.ConflictJobPayload{
		NodeID:          "node-a",
		TenantID:        "tenant-a",
		ConflictID:      "conflict-2",
		EventID:         localEvent.EventID,
		ResourceType:    "Patient",
		ResourceID:      "p1",
		LocalVersionID:  local.VersionID,
		RemoteVersionID: current.VersionID,
		Reason:          "stale base version",
		LocalEvent:      localEventJSON,
	})
	_ = jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      hasync.JobTypeConflictProcessing,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})

	processor := &hasync.JobProcessor{
		Engine: hasync.NewEngine(hasync.Config{
			NodeID:         "node-a",
			TenantID:       "tenant-a",
			Resources:      resources,
			History:        history,
			Conflicts:      conflicts,
			Jobs:           jobs,
			Audit:          audit,
			ConflictEngine: conflict.NewDefaultEngine(),
			Clock:          fixedClock(now),
		}),
		Jobs:  jobs,
		Clock: fixedClock(now),
	}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	found := false
	for _, rec := range audit.records {
		if rec.Action == hasync.AuditConflictNeedsReview {
			found = true
			if rec.Details["risk"] != string(conflict.RiskLevelReview) {
				t.Fatalf("expected review risk in details, got %+v", rec.Details)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected needs-review audit record, got %+v", audit.records)
	}
}

func TestConflictProcessingJobDefaultEngineAndHandler(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	resources := newMemResourceStore()
	history := newMemHistoryStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{}
	audit := &memAuditStore{}
	handler := &memResolutionHandler{}

	base := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "base-v1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
	}
	local := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "local-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","telecom":[{"system":"phone","value":"111"}]}`),
	}
	current := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "cloud-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","address":[{"city":"NYC"}]}`),
	}

	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     base,
	})
	_ = resources.Create(ctx, current)

	localEvent := hasync.LocalEvent{
		EventID:          "event-3",
		OriginNodeID:     "node-a",
		TenantID:         "tenant-a",
		ResourceType:     "Patient",
		ResourceID:       "p1",
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: base.VersionID,
		LocalVersion:     local.VersionID,
		ResourceAfter:    local,
	}
	localEventJSON, _ := json.Marshal(localEvent)
	payload, _ := json.Marshal(hasync.ConflictJobPayload{
		NodeID:          "node-a",
		TenantID:        "tenant-a",
		ConflictID:      "conflict-3",
		EventID:         localEvent.EventID,
		ResourceType:    "Patient",
		ResourceID:      "p1",
		LocalVersionID:  local.VersionID,
		RemoteVersionID: current.VersionID,
		Reason:          "stale base version",
		LocalEvent:      localEventJSON,
	})
	_ = jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      hasync.JobTypeConflictProcessing,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})

	processor := &hasync.JobProcessor{
		Engine: hasync.NewEngine(hasync.Config{
			NodeID:                    "node-a",
			TenantID:                  "tenant-a",
			Resources:                 resources,
			History:                   history,
			Conflicts:                 conflicts,
			Jobs:                      jobs,
			Audit:                     audit,
			ConflictResolutionHandler: handler,
			Clock:                     fixedClock(now),
		}),
		Jobs:  jobs,
		Clock: fixedClock(now),
	}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}
	if len(handler.calls) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(handler.calls))
	}
	if !handler.calls[0].AutoMergeable {
		t.Fatalf("expected handler to receive auto-merge result")
	}
	if len(handler.calls[0].Patch) == 0 {
		t.Fatalf("expected handler to receive a patch")
	}
	if len(handler.calls[0].Merged.JSON) == 0 {
		t.Fatalf("expected handler to receive merged JSON")
	}
}

func TestConflictProcessingJobHandlerForReview(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	resources := newMemResourceStore()
	history := newMemHistoryStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{}
	audit := &memAuditStore{}
	handler := &memResolutionHandler{}

	base := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "base-v1",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","birthDate":"2000-01-01"}`),
	}
	local := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "local-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","birthDate":"2000-02-02"}`),
	}
	current := &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "cloud-v2",
		JSON:         []byte(`{"resourceType":"Patient","id":"p1","telecom":[{"system":"phone","value":"111"}]}`),
	}

	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: base.ResourceType,
		ID:           base.ID,
		VersionID:    base.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     base,
	})
	_ = resources.Create(ctx, current)

	localEvent := hasync.LocalEvent{
		EventID:          "event-4",
		OriginNodeID:     "node-a",
		TenantID:         "tenant-a",
		ResourceType:     "Patient",
		ResourceID:       "p1",
		Operation:        hasync.EventTypeResourceUpdated,
		BaseCloudVersion: base.VersionID,
		LocalVersion:     local.VersionID,
		ResourceAfter:    local,
	}
	localEventJSON, _ := json.Marshal(localEvent)
	payload, _ := json.Marshal(hasync.ConflictJobPayload{
		NodeID:          "node-a",
		TenantID:        "tenant-a",
		ConflictID:      "conflict-4",
		EventID:         localEvent.EventID,
		ResourceType:    "Patient",
		ResourceID:      "p1",
		LocalVersionID:  local.VersionID,
		RemoteVersionID: current.VersionID,
		Reason:          "stale base version",
		LocalEvent:      localEventJSON,
	})
	_ = jobs.Enqueue(ctx, store.JobRecord{
		ID:        uuid.NewString(),
		Type:      hasync.JobTypeConflictProcessing,
		Payload:   payload,
		Status:    store.JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})

	processor := &hasync.JobProcessor{
		Engine: hasync.NewEngine(hasync.Config{
			NodeID:                    "node-a",
			TenantID:                  "tenant-a",
			Resources:                 resources,
			History:                   history,
			Conflicts:                 conflicts,
			Jobs:                      jobs,
			Audit:                     audit,
			ConflictResolutionHandler: handler,
			Clock:                     fixedClock(now),
		}),
		Jobs:  jobs,
		Clock: fixedClock(now),
	}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}
	if len(handler.calls) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(handler.calls))
	}
	if handler.calls[0].AutoMergeable {
		t.Fatalf("expected handler to receive non-auto-merge result")
	}
	if handler.calls[0].Review.Reason == "" {
		t.Fatalf("expected review reason in handler result")
	}
}
