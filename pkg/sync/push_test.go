package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestPushAcceptedCreate(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	hub := newMemHub()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	res := sampleResource("p1", "local-v1")
	_, _ = events.Append(ctx, store.ResourceEvent{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.EventActionCreate,
		Timestamp:    now,
		Hash:         res.Hash,
	})
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     res,
		Hash:         res.Hash,
	})

	engine := hasync.NewEngine(hasync.Config{
		NodeID:   "node-a",
		TenantID: "tenant-a",
		Events:   events,
		Cursors:  cursors,
		History:  history,
		Hub:      hub,
		Clock:    fixedClock(now),
	})

	summary, err := engine.Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if summary.Proposed != 1 || len(summary.Results) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Results[0].State != hasync.AckAccepted {
		t.Fatalf("state = %q", summary.Results[0].State)
	}
	if summary.Cursor != 1 {
		t.Fatalf("cursor = %d, want 1", summary.Cursor)
	}
}

func TestHubPushAlreadyProcessedDedupe(t *testing.T) {
	ctx := context.Background()
	hub := newMemHub()
	now := time.Now().UTC()
	res := sampleResource("p1", "v1")

	event := hasync.LocalEvent{
		EventID:       "fixed-event-id",
		OriginNodeID:  "node-a",
		TenantID:      "tenant-a",
		ResourceType:  res.ResourceType,
		ResourceID:    res.ID,
		Operation:     hasync.EventTypeResourceCreated,
		LocalVersion:  res.VersionID,
		ResourceAfter: res,
		ResourceHash:  res.Hash,
		CreatedAt:     now,
	}

	first, err := hub.Push(ctx, []hasync.LocalEvent{event})
	if err != nil || first[0].State != hasync.AckAccepted {
		t.Fatalf("first push: %+v err=%v", first, err)
	}
	second, err := hub.Push(ctx, []hasync.LocalEvent{event})
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if second[0].State != hasync.AckAlreadyProcessed {
		t.Fatalf("state = %q, want already_processed", second[0].State)
	}
}

func TestPushStaleBaseConflict(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{}
	hub := newMemHub()
	hub.staleOnMismatch = true
	now := time.Now().UTC()

	cloud := sampleResource("p1", "cloud-v1")
	hub.resources[resourceKey(cloud.ResourceType, cloud.ID)] = sampleResource("p1", "cloud-v2")

	local := sampleResource("p1", "local-v2")
	_, _ = events.Append(ctx, store.ResourceEvent{
		ResourceType: local.ResourceType,
		ID:           local.ID,
		VersionID:    local.VersionID,
		Action:       store.EventActionUpdate,
		Timestamp:    now,
		Hash:         local.Hash,
	})
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: cloud.ResourceType,
		ID:           cloud.ID,
		VersionID:    cloud.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     cloud,
	})
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: local.ResourceType,
		ID:           local.ID,
		VersionID:    local.VersionID,
		Action:       store.VersionActionUpdate,
		Timestamp:    now,
		Resource:     local,
	})

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, Cursors: cursors, History: history,
		Hub: hub, Conflicts: conflicts, Jobs: jobs,
		Clock: fixedClock(now),
	}).Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if summary.Results[0].State != hasync.AckConflicted {
		t.Fatalf("state = %q", summary.Results[0].State)
	}
	if len(conflicts.records) != 1 {
		t.Fatalf("conflicts = %d, want 1", len(conflicts.records))
	}
	if len(jobs.jobs) != 1 || jobs.jobs[0].Type != hasync.JobTypeConflictProcessing {
		t.Fatalf("jobs = %+v", jobs.jobs)
	}
}

func TestPushRejectedInvalidWrite(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	hub := newMemHub()
	now := time.Now().UTC()

	// Update without hub resource and without payload in history -> rejected by hub.
	_, _ = events.Append(ctx, store.ResourceEvent{
		ResourceType: "Patient",
		ID:           "missing",
		VersionID:    "v1",
		Action:       store.EventActionUpdate,
		Timestamp:    now,
	})

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, History: history, Hub: hub,
		Clock: fixedClock(now),
	}).Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if summary.Results[0].State != hasync.AckConflicted {
		t.Fatalf("state = %q, want conflicted for missing resource", summary.Results[0].State)
	}
}
