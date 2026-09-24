package storetest_test

import (
	"context"
	"testing"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/testkit/storetest"
)

func TestSearchStoreAccumulatesMultipleValuesPerField(t *testing.T) {
	ctx := context.Background()
	s := storetest.NewSearchStore()
	rt, id := "Patient", "p1"
	if err := s.Index(ctx, store.SearchIndexEntry{
		ResourceType: rt, ID: id, Fields: map[string]string{"string.name": "Doe"},
	}); err != nil {
		t.Fatalf("index Doe: %v", err)
	}
	if err := s.Index(ctx, store.SearchIndexEntry{
		ResourceType: rt, ID: id, Fields: map[string]string{"string.name": "Jane"},
	}); err != nil {
		t.Fatalf("index Jane: %v", err)
	}
	doe, err := s.LookupMatch(ctx, store.SearchMatch{FieldKey: "string.name", Value: "Doe"})
	if err != nil || len(doe) != 1 || doe[0] != id {
		t.Fatalf("Doe lookup = %v, %v", doe, err)
	}
	jane, err := s.LookupMatch(ctx, store.SearchMatch{FieldKey: "string.name", Value: "Jane"})
	if err != nil || len(jane) != 1 || jane[0] != id {
		t.Fatalf("Jane lookup = %v, %v", jane, err)
	}
	if err := s.RemoveIndex(ctx, rt, id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	doe, _ = s.Lookup(ctx, "string.name", "Doe")
	if len(doe) != 0 {
		t.Fatalf("expected empty after remove, got %v", doe)
	}
}
