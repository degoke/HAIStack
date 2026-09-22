package researchutil

import (
	"testing"

	"github.com/degoke/haistack/pkg/types"
)

func TestMemoryResourceStoreCRUD(t *testing.T) {
	ctx := t.Context()
	s := NewMemoryResourceStore()
	env, err := ParseResource("Patient", []byte(`{"resourceType":"Patient","id":"p1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, env); err == nil {
		t.Fatal("expected duplicate create error")
	}
	got, err := s.Read(ctx, "Patient", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "p1" || got.ResourceType != "Patient" {
		t.Fatalf("read = %+v", got)
	}
	ids, err := s.ListIDs(ctx, "Patient", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("ids = %v", ids)
	}
	ok, err := s.Exists(ctx, "Patient", "p1")
	if err != nil || !ok {
		t.Fatalf("exists = %v, %v", ok, err)
	}
	env.JSON = append([]byte(nil), env.JSON...)
	if err := s.Update(ctx, env); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "Patient", "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, "Patient", "p1"); err == nil {
		t.Fatal("expected missing read error")
	}
}

func TestMemoryResourceStoreRejectsNil(t *testing.T) {
	s := NewMemoryResourceStore()
	if err := s.Create(t.Context(), (*types.ResourceEnvelope)(nil)); err == nil {
		t.Fatal("expected nil create error")
	}
}
