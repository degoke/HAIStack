package terminology

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestLocalServiceSubsumesWalksParents(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStore()
	scope := "s1"
	sys := "http://example.org/cs"
	if err := mem.ReplaceCodeSystem(ctx, scope, sys, "1", []store.TerminologyConceptRecord{
		{ScopeID: scope, SystemURL: sys, SystemVersion: "1", Code: "root", Active: true},
		{ScopeID: scope, SystemURL: sys, SystemVersion: "1", Code: "mid", ParentCode: "root", Active: true},
		{ScopeID: scope, SystemURL: sys, SystemVersion: "1", Code: "leaf", ParentCode: "mid", Active: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &LocalService{Store: mem, ScopeID: scope}
	ok, err := svc.Subsumes(ctx, SubsumesRequest{ScopeID: scope, System: sys, Version: "1", BroadCode: "root", NarrowCode: "leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected root subsumes leaf")
	}
	ok, err = svc.Subsumes(ctx, SubsumesRequest{ScopeID: scope, System: sys, Version: "1", BroadCode: "mid", NarrowCode: "leaf"})
	if err != nil || !ok {
		t.Fatalf("mid subsumes leaf: ok=%v err=%v", ok, err)
	}
	ok, err = svc.Subsumes(ctx, SubsumesRequest{ScopeID: scope, System: sys, Version: "1", BroadCode: "leaf", NarrowCode: "root"})
	if err != nil || ok {
		t.Fatalf("leaf should not subsume root: ok=%v err=%v", ok, err)
	}
}
