package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestEnrichLocalEventStableEventID(t *testing.T) {
	ctx := context.Background()
	history := newMemHistoryStore()
	res := sampleResource("p1", "v1")
	event := store.ResourceEvent{
		Sequence:     42,
		ResourceType: "Patient",
		ID:           "p1",
		VersionID:    "v1",
		Action:       store.EventActionCreate,
		Timestamp:    time.Now().UTC(),
		Hash:         res.Hash,
	}
	_ = history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    event.Timestamp,
		Resource:     res,
	})

	local, err := hasync.EnrichLocalEvent(ctx, event, "node-a", "tenant-a", history)
	if err != nil {
		t.Fatalf("EnrichLocalEvent: %v", err)
	}
	want := hasync.OutboxEventID("node-a", "tenant-a", 42)
	if local.EventID != want {
		t.Fatalf("event id = %q, want %q", local.EventID, want)
	}
}

func TestPushRepushSameOutboxEventIsIdempotent(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	hub := newMemHub()
	now := time.Now().UTC()

	res := sampleResource("p1", "v1")
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
	})

	cfg := hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, Cursors: cursors, History: history, Hub: hub,
		Clock: fixedClock(now),
	}
	if _, err := hasync.NewEngine(cfg).Push(ctx); err != nil {
		t.Fatalf("first push: %v", err)
	}
	_ = cursors.UpsertCursor(ctx, store.Cursor{Name: hasync.CursorPush, Position: "0", UpdatedAt: now})

	summary, err := hasync.NewEngine(cfg).Push(ctx)
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	if summary.Results[0].State != hasync.AckAlreadyProcessed {
		t.Fatalf("state = %q", summary.Results[0].State)
	}
}

func TestPushPartialBatchFailureDoesNotAdvanceCursor(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	hub := newMemHub()
	now := time.Now().UTC()

	for i, id := range []string{"p1", "p2"} {
		res := sampleResource(id, "v"+id)
		_, _ = events.Append(ctx, store.ResourceEvent{
			Sequence:     int64(i + 1),
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
		})
	}

	hub.pushErr = context.Canceled

	_, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, Cursors: cursors, History: history, Hub: hub,
		Clock: fixedClock(now),
	}).Push(ctx)
	if err == nil {
		t.Fatal("expected hub error")
	}
	cursor, _ := cursors.GetCursor(ctx, hasync.CursorPush)
	if cursor != nil {
		t.Fatalf("cursor advanced on hub failure: %+v", cursor)
	}
}

func TestPushNeedsRetryDoesNotAdvanceCursor(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	jobs := &memJobStore{}
	hub := &retryHub{inner: newMemHub()}
	now := time.Now().UTC()

	res := sampleResource("p1", "v1")
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
	})

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, Cursors: cursors, History: history, Hub: hub,
		Jobs: jobs, Clock: fixedClock(now),
	}).Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if summary.Results[0].State != hasync.AckNeedsRetry {
		t.Fatalf("state = %q", summary.Results[0].State)
	}
	cursor, _ := cursors.GetCursor(ctx, hasync.CursorPush)
	if cursor != nil {
		t.Fatalf("cursor advanced on retry: %+v", cursor)
	}
	if len(jobs.jobs) != 1 || jobs.jobs[0].Type != hasync.JobTypeRetryPush {
		t.Fatalf("jobs = %+v", jobs.jobs)
	}
}

func TestPushPreservesOutboxOrderWhenDeleteNeedsRetry(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	now := time.Now().UTC()
	hub := &orderedRetryHub{}

	deleteResource := sampleResource("p-delete", "delete-v1")
	deleteVersion := store.ResourceVersion{
		ResourceType: deleteResource.ResourceType,
		ID:           deleteResource.ID,
		VersionID:    "delete-v2",
		Action:       store.VersionActionDelete,
		Timestamp:    now,
		Deleted:      true,
	}
	if _, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: deleteResource.ResourceType,
		ID:           deleteResource.ID,
		VersionID:    deleteVersion.VersionID,
		Action:       store.EventActionDelete,
		Timestamp:    now,
		Hash:         deleteResource.Hash,
	}); err != nil {
		t.Fatalf("append delete event: %v", err)
	}
	if err := history.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: deleteResource.ResourceType,
		ID:           deleteResource.ID,
		VersionID:    deleteResource.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    now,
		Resource:     deleteResource,
		Hash:         deleteResource.Hash,
	}); err != nil {
		t.Fatalf("append delete base history: %v", err)
	}
	if err := history.AppendVersion(ctx, deleteVersion); err != nil {
		t.Fatalf("append delete history: %v", err)
	}

	updateResource := sampleResource("p-update", "update-v1")
	updated := sampleResource("p-update", "update-v2")
	if _, err := events.Append(ctx, store.ResourceEvent{
		ResourceType: updated.ResourceType,
		ID:           updated.ID,
		VersionID:    updated.VersionID,
		Action:       store.EventActionUpdate,
		Timestamp:    now,
		Hash:         updated.Hash,
	}); err != nil {
		t.Fatalf("append update event: %v", err)
	}
	for _, version := range []store.ResourceVersion{
		{
			ResourceType: updateResource.ResourceType,
			ID:           updateResource.ID,
			VersionID:    updateResource.VersionID,
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     updateResource,
			Hash:         updateResource.Hash,
		},
		{
			ResourceType: updated.ResourceType,
			ID:           updated.ID,
			VersionID:    updated.VersionID,
			Action:       store.VersionActionUpdate,
			Timestamp:    now,
			Resource:     updated,
			Hash:         updated.Hash,
		},
	} {
		if err := history.AppendVersion(ctx, version); err != nil {
			t.Fatalf("append update history: %v", err)
		}
	}

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, History: history, Cursors: cursors, Hub: hub,
		Clock: fixedClock(now),
	}).Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(hub.events) != 2 || hub.events[0].Operation != hasync.EventTypeResourceDeleted || hub.events[1].Operation != hasync.EventTypeResourceUpdated {
		t.Fatalf("hub event order = %+v, want delete then update", hub.events)
	}
	if summary.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0 because the first event needs retry", summary.Cursor)
	}
	if cursor, _ := cursors.GetCursor(ctx, hasync.CursorPush); cursor != nil {
		t.Fatalf("cursor persisted despite retry: %+v", cursor)
	}
}

func TestPushConflictJobEnqueueFailureStopsCursorAdvance(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	conflicts := &memConflictStore{}
	jobs := &memJobStore{enqueueErr: errEnqueueFailed}
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

	_, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: events, Cursors: cursors, History: history,
		Hub: hub, Conflicts: conflicts, Jobs: jobs,
		Clock: fixedClock(now),
	}).Push(ctx)
	if err == nil {
		t.Fatal("expected enqueue failure")
	}
	cursor, _ := cursors.GetCursor(ctx, hasync.CursorPush)
	if cursor != nil {
		t.Fatalf("cursor advanced despite enqueue failure: %+v", cursor)
	}
}

type retryHub struct {
	inner *memHub
}

type orderedRetryHub struct {
	events []hasync.LocalEvent
}

func (h *orderedRetryHub) Push(_ context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	h.events = append(h.events, events...)
	results := make([]hasync.PushResult, 0, len(events))
	for _, event := range events {
		result := hasync.PushResult{EventID: event.EventID, State: hasync.AckAccepted}
		if event.Operation == hasync.EventTypeResourceDeleted {
			result.State = hasync.AckNeedsRetry
		}
		results = append(results, result)
	}
	return results, nil
}

func (h *orderedRetryHub) Pull(context.Context, int64, int) ([]hasync.CanonicalEvent, error) {
	return nil, nil
}

func (h *retryHub) Push(ctx context.Context, events []hasync.LocalEvent) ([]hasync.PushResult, error) {
	results, err := h.inner.Push(ctx, events)
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].State = hasync.AckNeedsRetry
		results[i].RetryAfter = time.Now().UTC().Add(time.Minute)
	}
	return results, nil
}

func (h *retryHub) Pull(ctx context.Context, afterSequence int64, limit int) ([]hasync.CanonicalEvent, error) {
	return h.inner.Pull(ctx, afterSequence, limit)
}

func TestJobProcessorRetryPush(t *testing.T) {
	ctx := context.Background()
	jobs := &memJobStore{}
	now := time.Now().UTC()
	_ = jobs.Enqueue(ctx, store.JobRecord{
		ID: "job-1", Type: hasync.JobTypeRetryPush, Status: store.JobStatusPending,
		CreatedAt: now, UpdatedAt: now,
	})

	engine := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Events: &memEventStore{}, Hub: newMemHub(), Clock: fixedClock(now),
	})
	processor := &hasync.JobProcessor{Engine: engine, Jobs: jobs, Clock: fixedClock(now)}

	processed, err := processor.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatal("expected job to be processed")
	}
}
