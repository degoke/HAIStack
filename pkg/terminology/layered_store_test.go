package terminology

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestLayeredStoreGlobalCodeSystemTenantValueSet(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	tenantVS := []byte(`{"resourceType":"ValueSet","url":"urn:tenant-vs","version":"1","compose":{"include":[{"system":"urn:global","concept":[{"code":"x"}]}]}}`)
	if err := Compile(ctx, m, "tenant-a", "", tenantVS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "ValueSet", CanonicalURL: "urn:tenant-vs", Version: "1", ResourceJSON: tenantVS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	svc := NewLocalService(layered, "tenant-a")
	got, err := svc.Lookup(ctx, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || !got.Found || got.Concept.Display != "Global" {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	ex, err := svc.Expand(ctx, ExpandRequest{URL: "urn:tenant-vs"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "x" {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
}

func TestChainTenantThenGlobal(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:chain","version":"1","concept":[{"code":"a","display":"A"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:chain", Version: "1", ResourceJSON: cs,
	})
	tenant := NewLocalService(NewLayeredStore(m, "tenant-a"), "tenant-a")
	global := NewLocalService(m, GlobalScopeID)
	chain := Chain{Providers: []Provider{tenant, global}}
	got, err := chain.Lookup(ctx, LookupRequest{System: "urn:chain", Code: "a"})
	if err != nil || !got.Found {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
}
