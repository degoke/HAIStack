package sync_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
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
			Operation:         hasync.EventTypeResourceCreated,
			ResourceAfter:     sampleResource("p1", "v1"),
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

func TestPullIndexesAllEntriesInOneTransaction(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	res := sampleResource("p1", "canonical-v1")
	hub := newMemHub()
	hub.canonical = []hasync.CanonicalEvent{{
		TenantID:           "tenant-a",
		ResourceType:       res.ResourceType,
		ResourceID:         res.ID,
		Operation:          hasync.EventTypeResourceCreated,
		ResourceAfter:      res,
		CanonicalSequence:  1,
		CanonicalVersionID: res.VersionID,
		Status:             hasync.CanonicalStatusAccepted,
	}}

	engine := hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Cursors: db.CursorStore(), Inbox: db.InboxStore(),
		Resources: db.ResourceStore(), History: db.HistoryStore(),
		Search: db.SearchStore(), Sessions: db,
		SearchIndexer: multiEntryIndexer{}, Hub: hub,
		Clock: fixedClock(time.Now().UTC()),
	})
	if _, err := engine.Pull(ctx); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	for _, match := range []store.SearchMatch{
		{ResourceType: "Patient", FieldKey: "string.family", Value: "Smith"},
		{ResourceType: "Patient", FieldKey: "token.identifier", Value: "abc"},
	} {
		ids, err := db.SearchStore().LookupMatch(ctx, match)
		if err != nil {
			t.Fatalf("LookupMatch(%+v): %v", match, err)
		}
		if len(ids) != 1 || ids[0] != "p1" {
			t.Fatalf("LookupMatch(%+v) = %v, want [p1]", match, ids)
		}
	}
}

func TestPullTransactionRollsBackResourceHistoryAndInbox(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	res := sampleResource("p1", "canonical-v1")
	hub := newMemHub()
	hub.canonical = []hasync.CanonicalEvent{{
		TenantID:           "tenant-a",
		ResourceType:       res.ResourceType,
		ResourceID:         res.ID,
		Operation:          hasync.EventTypeResourceCreated,
		ResourceAfter:      res,
		CanonicalSequence:  1,
		CanonicalVersionID: res.VersionID,
		Status:             hasync.CanonicalStatusAccepted,
	}}

	_, err = hasync.NewEngine(hasync.Config{
		NodeID: "node-a", TenantID: "tenant-a",
		Cursors: db.CursorStore(), Inbox: db.InboxStore(),
		Resources: db.ResourceStore(), History: db.HistoryStore(),
		Search: db.SearchStore(), Sessions: db,
		SearchIndexer: failingIndexer{}, Hub: hub,
		Clock: fixedClock(time.Now().UTC()),
	}).Pull(ctx)
	if err == nil {
		t.Fatal("expected search indexing failure")
	}
	if exists, err := db.ResourceStore().Exists(ctx, "Patient", "p1"); err != nil {
		t.Fatalf("resource exists check: %v", err)
	} else if exists {
		t.Fatal("resource remained after failed transactional pull")
	}
	if versions, err := db.HistoryStore().GetHistory(ctx, "Patient", "p1"); err != nil {
		t.Fatalf("history read: %v", err)
	} else if len(versions) != 0 {
		t.Fatalf("history remained after failed transactional pull: %+v", versions)
	}
	applied, err := db.InboxStore().IsApplied(ctx, hasync.CanonicalEventID("tenant-a", 1))
	if err != nil {
		t.Fatalf("inbox read: %v", err)
	}
	if applied {
		t.Fatal("inbox marker remained after failed transactional pull")
	}
}

type multiEntryIndexer struct{}

func (multiEntryIndexer) BuildSearchEntries(context.Context, *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	return []store.SearchIndexEntry{
		{Fields: map[string]string{"string.family": "Smith"}},
		{Fields: map[string]string{"token.identifier": "abc"}},
	}, nil
}

type failingIndexer struct{}

func (failingIndexer) BuildSearchEntries(context.Context, *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	return nil, errors.New("indexer failed")
}
