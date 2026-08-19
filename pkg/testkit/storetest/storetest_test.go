package storetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/testkit/factories"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
)

func TestResourceStoreStrictSemantics(t *testing.T) {
	ctx := context.Background()
	s := storetest.NewResourceStore()
	res, err := factories.NewPatient(factories.WithPatientID("p1"))
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	if err := s.Create(ctx, res); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Read(ctx, "Patient", "missing"); err == nil {
		t.Fatal("expected not found error")
	}
	if err := s.Create(ctx, res); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestResourceStoreLenientRead(t *testing.T) {
	ctx := context.Background()
	s := storetest.NewLenientResourceStore()
	got, err := s.Read(ctx, "Patient", "missing")
	if err != nil || got != nil {
		t.Fatalf("lenient read = %v, %v", got, err)
	}
}

func TestResourceStoreListIDsIsStableAndPaged(t *testing.T) {
	ctx := context.Background()
	s := storetest.NewResourceStore()
	for _, id := range []string{"p3", "p1", "p2"} {
		res, err := factories.NewPatient(factories.WithPatientID(id))
		if err != nil {
			t.Fatalf("NewPatient: %v", err)
		}
		if err := s.Create(ctx, res); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	ids, err := s.ListIDs(ctx, "Patient", 2, 0)
	if err != nil || len(ids) != 2 || ids[0] != "p1" || ids[1] != "p2" {
		t.Fatalf("first page = %v, %v", ids, err)
	}
	ids, err = s.ListIDs(ctx, "Patient", 2, 2)
	if err != nil || len(ids) != 1 || ids[0] != "p3" {
		t.Fatalf("second page = %v, %v", ids, err)
	}
	ids, err = s.ListIDs(ctx, "Patient", 0, 0)
	if err != nil || len(ids) != 3 {
		t.Fatalf("default page = %v, %v", ids, err)
	}
}

func TestResourceStoreReadsCopiesAndRejectsNil(t *testing.T) {
	ctx := context.Background()
	s := storetest.NewResourceStore()
	res, err := factories.NewPatient(factories.WithPatientID("p1"))
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	if err := s.Create(ctx, res); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Read(ctx, "Patient", "p1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got.JSON[0] = 'X'
	again, err := s.Read(ctx, "Patient", "p1")
	if err != nil {
		t.Fatalf("Read again: %v", err)
	}
	if again.JSON[0] != '{' {
		t.Fatal("read returned aliased JSON")
	}
	if err := s.Create(ctx, nil); err == nil {
		t.Fatal("expected nil create error")
	}
	if err := s.Update(ctx, nil); err == nil {
		t.Fatal("expected nil update error")
	}
}

func TestEventStoreMonotonicSequence(t *testing.T) {
	ctx := context.Background()
	events := storetest.NewEventStore()
	now := time.Now().UTC()
	e1, _ := events.Append(ctx, store.ResourceEvent{ResourceType: "Patient", ID: "p1", Timestamp: now})
	e2, _ := events.Append(ctx, store.ResourceEvent{ResourceType: "Patient", ID: "p2", Timestamp: now})
	if e1.Sequence != 1 || e2.Sequence != 2 {
		t.Fatalf("sequences = %d, %d", e1.Sequence, e2.Sequence)
	}
	since, err := events.ReadSince(ctx, 0, 10)
	if err != nil || len(since) != 2 {
		t.Fatalf("ReadSince = %d, %v", len(since), err)
	}
}

func TestConflictStoreListByResource(t *testing.T) {
	ctx := context.Background()
	conflicts := storetest.NewConflictStore()
	now := time.Now().UTC()
	_ = conflicts.Append(ctx, store.ConflictRecord{ID: "c1", ResourceType: "Patient", ResourceID: "p1", CreatedAt: now})
	_ = conflicts.Append(ctx, store.ConflictRecord{ID: "c2", ResourceType: "Patient", ResourceID: "p1", CreatedAt: now})
	list, err := conflicts.List(ctx, "Patient", "p1")
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
}

func TestConflictStoreOrdersAndRejectsDuplicateIDs(t *testing.T) {
	ctx := context.Background()
	conflicts := storetest.NewConflictStore()
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	if err := conflicts.Append(ctx, store.ConflictRecord{ID: "later", ResourceType: "Patient", ResourceID: "p1", CreatedAt: second}); err != nil {
		t.Fatalf("append later: %v", err)
	}
	if err := conflicts.Append(ctx, store.ConflictRecord{ID: "earlier", ResourceType: "Patient", ResourceID: "p1", CreatedAt: first}); err != nil {
		t.Fatalf("append earlier: %v", err)
	}
	if err := conflicts.Append(ctx, store.ConflictRecord{ID: "earlier", ResourceType: "Patient", ResourceID: "p1"}); err == nil {
		t.Fatal("expected duplicate conflict error")
	}
	list, err := conflicts.List(ctx, "Patient", "p1")
	if err != nil || len(list) != 2 || list[0].ID != "earlier" {
		t.Fatalf("ordered conflicts = %+v, %v", list, err)
	}
}

func TestAuditStoreSupportsAllQueryFilters(t *testing.T) {
	ctx := context.Background()
	audit := storetest.NewAuditStore()
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, record := range []store.AuditRecord{
		{ID: "a2", Timestamp: base.Add(2 * time.Hour), Actor: "agent", Action: "write", Outcome: "success", Tenant: "t1", Subject: "s1", ToolName: "tool", ConversationID: "c1"},
		{ID: "a1", Timestamp: base.Add(time.Hour), Actor: "agent", Action: "read", Outcome: "success", Tenant: "t1", Subject: "s1", ToolName: "tool", ConversationID: "c1"},
		{ID: "a3", Timestamp: base.Add(3 * time.Hour), Actor: "other", Action: "write", Outcome: "error", Tenant: "t2"},
	} {
		record.ResourceType = "Patient"
		record.ResourceID = "p1"
		record.Details = map[string]string{"index": string(rune('0' + i))}
		if err := audit.Append(ctx, record); err != nil {
			t.Fatalf("append audit: %v", err)
		}
	}
	list, err := audit.List(ctx, store.AuditQuery{
		ResourceType: "Patient", ResourceID: "p1", Actor: "agent", Action: "write",
		Outcome: "success", Tenant: "t1", Subject: "s1", ToolName: "tool", ConversationID: "c1",
		After: base, Before: base.Add(2 * time.Hour), Limit: 1,
	})
	if err != nil || len(list) != 1 || list[0].ID != "a2" {
		t.Fatalf("filtered audit = %+v, %v", list, err)
	}
}

func TestWriteSessionRollbackAndCommit(t *testing.T) {
	ctx := context.Background()
	provider := storetest.NewWriteSessionProvider()
	res, err := factories.NewPatient(factories.WithPatientID("p1"))
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	session, err := provider.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite: %v", err)
	}
	if err := session.ResourceStore().Create(ctx, res); err != nil {
		t.Fatalf("session create: %v", err)
	}
	if err := session.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if exists, _ := provider.Resources.Exists(ctx, "Patient", "p1"); exists {
		t.Fatal("rolled-back resource was committed")
	}

	session, err = provider.BeginWrite(ctx)
	if err != nil {
		t.Fatalf("BeginWrite commit: %v", err)
	}
	if err := session.ResourceStore().Create(ctx, res); err != nil {
		t.Fatalf("session create commit: %v", err)
	}
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if exists, _ := provider.Resources.Exists(ctx, "Patient", "p1"); !exists {
		t.Fatal("committed resource was not persisted")
	}
	if err := session.Commit(ctx); err == nil {
		t.Fatal("expected second commit error")
	}
}

func TestSearchStoreRemovesAndReplacesIndexes(t *testing.T) {
	ctx := context.Background()
	search := storetest.NewSearchStore()
	if err := search.Index(ctx, store.SearchIndexEntry{ResourceType: "Patient", ID: "p1", Fields: map[string]string{"name": "Jane"}}); err != nil {
		t.Fatalf("index p1: %v", err)
	}
	if err := search.Index(ctx, store.SearchIndexEntry{ResourceType: "Patient", ID: "p2", Fields: map[string]string{"name": "Jane"}}); err != nil {
		t.Fatalf("index p2: %v", err)
	}
	if err := search.RemoveIndex(ctx, "Patient", "p2"); err != nil {
		t.Fatalf("remove p2: %v", err)
	}
	ids, err := search.Lookup(ctx, "name", "Jane")
	if err != nil || len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("lookup after remove = %v, %v", ids, err)
	}
	if err := search.RemoveIndex(ctx, "Patient", "p1"); err != nil {
		t.Fatalf("remove p1: %v", err)
	}
	if err := search.Index(ctx, store.SearchIndexEntry{ResourceType: "Patient", ID: "p1", Fields: map[string]string{"name": "Janet"}}); err != nil {
		t.Fatalf("replace p1: %v", err)
	}
	old, _ := search.Lookup(ctx, "name", "Jane")
	newIDs, _ := search.Lookup(ctx, "name", "Janet")
	if len(old) != 0 || len(newIDs) != 1 || newIDs[0] != "p1" {
		t.Fatalf("replaced lookup = old %v new %v", old, newIDs)
	}
	if _, err := search.QueryPrepared(ctx, store.PreparedQuery{Name: "unknown"}, nil); err == nil {
		t.Fatal("expected unknown prepared query error")
	}
}

func TestSearchStoreAggregatesTypedFields(t *testing.T) {
	ctx := context.Background()
	search := storetest.NewSearchStore()
	for _, entry := range []store.SearchIndexEntry{
		{ResourceType: "Patient", ID: "p1", Fields: map[string]string{"name": "Jane"}},
		{ResourceType: "Patient", ID: "p1", Fields: map[string]string{"gender": "female"}},
		{ResourceType: "Observation", ID: "o1", Fields: map[string]string{"name": "Jane"}},
	} {
		if err := search.Index(ctx, entry); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}
	ids, err := search.LookupMatch(ctx, store.SearchMatch{ResourceType: "Patient", FieldKey: "name", Value: "Jane", Operator: "="})
	if err != nil || len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("LookupMatch = %v, %v", ids, err)
	}
	values, err := search.FieldValues(ctx, "Patient", "gender", []string{"p1"})
	if err != nil || values["p1"] != "female" {
		t.Fatalf("FieldValues = %v, %v", values, err)
	}
}

func TestDeviceBackendWiring(t *testing.T) {
	backend := storetest.NewDeviceBackend()
	if backend.Resources == nil || backend.Events == nil || backend.Jobs == nil {
		t.Fatal("backend stores not initialized")
	}
}

func TestSearchStoreLookup(t *testing.T) {
	ctx := context.Background()
	search := storetest.NewSearchStore()
	_ = search.Index(ctx, store.SearchIndexEntry{
		ResourceType: "Patient",
		ID:           "p1",
		Fields:       map[string]string{"name": "Jane"},
	})
	ids, err := search.Lookup(ctx, "name", "Jane")
	if err != nil || len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("Lookup = %v, %v", ids, err)
	}
}
