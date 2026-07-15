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
