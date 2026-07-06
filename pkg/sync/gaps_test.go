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

type retryHub struct {
	inner *memHub
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
