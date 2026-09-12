package registry

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/terminology"
)

func TestTerminologyTargetRoutesCodeSystemToGlobal(t *testing.T) {
	mgr := NewManager(Config{
		Terminology:       terminology.NewMemoryStore(),
		GlobalTerminology: terminology.NewMemoryStore(),
		TerminologyScope:  "tenant-a",
	})
	store, scope := mgr.terminologyTarget("CodeSystem")
	if scope != terminology.GlobalScopeID {
		t.Fatalf("scope=%q want global", scope)
	}
	if store != mgr.globalTerminology {
		t.Fatal("expected global terminology store for CodeSystem")
	}
}

func TestTerminologyTargetRoutesValueSetToGlobal(t *testing.T) {
	mgr := NewManager(Config{
		Terminology:       terminology.NewMemoryStore(),
		GlobalTerminology: terminology.NewMemoryStore(),
		TerminologyScope:  "tenant-a",
	})
	store, scope := mgr.terminologyTarget("ValueSet")
	if scope != terminology.GlobalScopeID {
		t.Fatalf("scope=%q want global", scope)
	}
	if store != mgr.globalTerminology {
		t.Fatal("expected global terminology store for ValueSet")
	}
}
