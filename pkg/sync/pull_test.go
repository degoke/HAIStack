package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestPullOrderedCanonicalReplay(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	history := newMemHistoryStore()
	inbox := newMemInboxStore()
	cursors := newMemCursorStore()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	hub := newMemHub()
	res := sampleResource("p1", "canonical-v1")
	hub.canonical = []hasync.CanonicalEvent{{
		TenantID:           "tenant-a",
		ResourceType:       res.ResourceType,
		ResourceID:         res.ID,
		Operation:          hasync.EventTypeResourceCreated,
		ResourceAfter:      res,
		ResourceHash:       res.Hash,
		CanonicalSequence:  1,
		CanonicalVersionID: "canonical-v1",
		Status:             hasync.CanonicalStatusAccepted,
	}}

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Resources: resources, History: history, Inbox: inbox, Cursors: cursors,
		Hub: hub, Clock: fixedClock(now),
	}).Pull(ctx)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if summary.Applied != 1 || summary.Cursor != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	exists, _ := resources.Exists(ctx, "Patient", "p1")
	if !exists {
		t.Fatal("resource not applied locally")
	}
}

func TestPullIdempotentInbox(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	history := newMemHistoryStore()
	inbox := newMemInboxStore()
	cursors := newMemCursorStore()
	now := time.Now().UTC()

	hub := newMemHub()
	res := sampleResource("p1", "canonical-v1")
	hub.canonical = []hasync.CanonicalEvent{{
		ResourceType:       res.ResourceType,
		ResourceID:         res.ID,
		Operation:          hasync.EventTypeResourceCreated,
		ResourceAfter:      res,
		CanonicalSequence:  1,
		CanonicalVersionID: "canonical-v1",
		Status:             hasync.CanonicalStatusAccepted,
	}}

	cfg := hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Resources: resources, History: history, Inbox: inbox, Cursors: cursors,
		Hub: hub, Clock: fixedClock(now),
	}
	engine := hasync.NewEngine(cfg)

	first, err := engine.Pull(ctx)
	if err != nil || first.Applied != 1 {
		t.Fatalf("first pull: %+v err=%v", first, err)
	}

	_ = cursors.UpsertCursor(ctx, store.Cursor{Name: hasync.CursorPull, Position: "0", UpdatedAt: now})
	second, err := engine.Pull(ctx)
	if err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if second.Skipped != 1 || second.Applied != 0 {
		t.Fatalf("second summary = %+v", second)
	}
}

func TestPullTombstoneDeleteApply(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	history := newMemHistoryStore()
	inbox := newMemInboxStore()
	cursors := newMemCursorStore()
	now := time.Now().UTC()

	res := sampleResource("p1", "canonical-v1")
	_ = resources.Create(ctx, res)

	hub := newMemHub()
	hub.canonical = []hasync.CanonicalEvent{{
		ResourceType:       res.ResourceType,
		ResourceID:         res.ID,
		Operation:          hasync.EventTypeResourceDeleted,
		ResourceHash:       res.Hash,
		CanonicalSequence:  2,
		CanonicalVersionID: "canonical-delete",
		Status:             hasync.CanonicalStatusAccepted,
	}}

	summary, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Resources: resources, History: history, Inbox: inbox, Cursors: cursors,
		Hub: hub, Clock: fixedClock(now),
	}).Pull(ctx)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if summary.Applied != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	exists, _ := resources.Exists(ctx, "Patient", "p1")
	if exists {
		t.Fatal("resource should be deleted locally")
	}
	versions, _ := history.GetHistory(ctx, "Patient", "p1")
	if len(versions) != 1 || !versions[0].Deleted {
		t.Fatalf("history = %+v", versions)
	}
}

func TestPullCursorAdvancesOnlyAfterSuccessfulApply(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	history := newMemHistoryStore()
	inbox := newMemInboxStore()
	cursors := newMemCursorStore()
	now := time.Now().UTC()

	hub := newMemHub()
	hub.canonical = []hasync.CanonicalEvent{
		{
			ResourceType: "Patient", ResourceID: "p1",
			Operation: hasync.EventTypeResourceCreated,
			ResourceAfter: sampleResource("p1", "v1"),
			CanonicalSequence: 1, CanonicalVersionID: "v1",
			Status: hasync.CanonicalStatusAccepted,
		},
		{
			ResourceType: "Patient", ResourceID: "p2",
			Operation:         hasync.EventTypeResourceUpdated,
			ResourceAfter:     nil, // will fail apply
			CanonicalSequence: 2, CanonicalVersionID: "v2",
			Status: hasync.CanonicalStatusAccepted,
		},
	}

	_, err := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Resources: resources, History: history, Inbox: inbox, Cursors: cursors,
		Hub: hub, Clock: fixedClock(now),
	}).Pull(ctx)
	if err == nil {
		t.Fatal("expected apply error")
	}
	cursor, _ := cursors.GetCursor(ctx, hasync.CursorPull)
	if cursor != nil && cursor.Position != "1" {
		t.Fatalf("cursor = %+v, want position 1 or unset", cursor)
	}
}
