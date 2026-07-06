package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
)

func TestEngineSyncOnceRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	res := sampleResource("p1", "local-v1")

	writeResult, err := db.ApplyLocalWrite(ctx, sqlite.LocalWrite{
		Resource: res,
		Action:   store.VersionActionCreate,
		Version: store.ResourceVersion{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.VersionActionCreate,
			Timestamp:    now,
			Resource:     res,
			Hash:         res.Hash,
		},
		Event: store.ResourceEvent{
			ResourceType: res.ResourceType,
			ID:           res.ID,
			VersionID:    res.VersionID,
			Action:       store.EventActionCreate,
			Timestamp:    now,
			Hash:         res.Hash,
		},
	})
	if err != nil {
		t.Fatalf("ApplyLocalWrite: %v", err)
	}

	hub := newMemHub()
	engine := hasync.NewEngine(hasync.Config{
		NodeID:    "device-1",
		TenantID:  "tenant-a",
		Events:    db.OutboxStore(),
		Cursors:   db.CursorStore(),
		Inbox:     db.InboxStore(),
		Resources: db.ResourceStore(),
		History:   db.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	})

	push, pull, err := engine.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if push.Proposed != 1 || push.Results[0].State != hasync.AckAccepted {
		t.Fatalf("push = %+v", push)
	}
	_ = writeResult

	// Simulate second device pulling canonical state from hub into fresh sqlite db.
	db2, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite2: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if err := db2.Migrate(ctx); err != nil {
		t.Fatalf("migrate2: %v", err)
	}

	pullSummary, err := hasync.NewEngine(hasync.Config{
		NodeID:    "device-2",
		TenantID:  "tenant-a",
		Cursors:   db2.CursorStore(),
		Inbox:     db2.InboxStore(),
		Resources: db2.ResourceStore(),
		History:   db2.HistoryStore(),
		Hub:       hub,
		Clock:     fixedClock(now),
	}).Pull(ctx)
	if err != nil {
		t.Fatalf("pull device2: %v", err)
	}
	if pullSummary.Applied != 1 {
		t.Fatalf("pull summary = %+v", pullSummary)
	}

	exists, err := db2.ResourceStore().Exists(ctx, "Patient", "p1")
	if err != nil || !exists {
		t.Fatalf("device2 resource exists = %v err=%v", exists, err)
	}
	_ = pull
}

func TestRepeatedSyncAttemptsStayIdempotent(t *testing.T) {
	ctx := context.Background()
	events := &memEventStore{}
	history := newMemHistoryStore()
	cursors := newMemCursorStore()
	inbox := newMemInboxStore()
	resources := newMemResourceStore()
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
		Events: events, Cursors: cursors, Inbox: inbox,
		Resources: resources, History: history, Hub: hub,
		Clock: fixedClock(now),
	}
	engine := hasync.NewEngine(cfg)

	if _, err := engine.Push(ctx); err != nil {
		t.Fatalf("push: %v", err)
	}
	if _, pull, err := engine.SyncOnce(ctx); err != nil {
		t.Fatalf("sync once: %v", err)
	} else if pull.Applied == 0 && pull.Skipped == 0 && pull.Fetched == 0 {
		// acceptable when hub already drained
	}
	if _, pull, err := engine.SyncOnce(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	} else if pull.Fetched > 0 && pull.Applied > 0 {
		t.Fatalf("expected idempotent pull skip, got %+v", pull)
	}
}
