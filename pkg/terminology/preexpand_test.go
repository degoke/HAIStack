package terminology

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestPreExpandSkipsAlreadyExpanded(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a","display":"A"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "1", ResourceJSON: vsJSON,
	}); err != nil {
		t.Fatal(err)
	}
	res1, err := PreExpandValueSet(ctx, m, GlobalScopeID, "urn:vs", "1", nil, PreExpandOptions{})
	if err != nil || res1.Skipped || res1.Members != 1 {
		t.Fatalf("first expand=%+v err=%v", res1, err)
	}
	res2, err := PreExpandValueSet(ctx, m, GlobalScopeID, "urn:vs", "1", nil, PreExpandOptions{})
	if err != nil || !res2.Skipped || res2.Reason != SkipAlreadyExpanded {
		t.Fatalf("second expand=%+v err=%v", res2, err)
	}
}

func TestPreExpandSkipsServerExpansion(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","compose":{"include":[{"system":"urn:cs"}]},"expansion":{"contains":[{"system":"urn:cs","code":"a","display":"A"}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "1", ResourceJSON: vsJSON,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := PreExpandValueSet(ctx, m, GlobalScopeID, "urn:vs", "1", vsJSON, PreExpandOptions{})
	if err != nil || !res.Skipped || res.Reason != SkipServerExpansion {
		t.Fatalf("expand=%+v err=%v", res, err)
	}
	members, err := m.ListValueSetMembers(ctx, GlobalScopeID, "urn:vs", "1")
	if err != nil || len(members) != 1 {
		t.Fatalf("members=%d err=%v", len(members), err)
	}
}

func TestPreExpandSkipsTooCostly(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	var concepts []map[string]any
	for i := 0; i < 5; i++ {
		concepts = append(concepts, map[string]any{"code": fmt.Sprintf("c%d", i), "display": "X"})
	}
	cs := map[string]any{"resourceType": "CodeSystem", "url": "urn:big", "version": "1", "concept": concepts}
	csRaw, _ := json.Marshal(cs)
	if err := Compile(ctx, m, GlobalScopeID, "", csRaw); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:big", Version: "1", ResourceJSON: csRaw,
	})
	vs := map[string]any{"resourceType": "ValueSet", "url": "urn:big-vs", "version": "1", "compose": map[string]any{"include": []any{map[string]any{"system": "urn:big"}}}}
	vsRaw, _ := json.Marshal(vs)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:big-vs", Version: "1", ResourceJSON: vsRaw,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := PreExpandValueSet(ctx, m, GlobalScopeID, "urn:big-vs", "1", nil, PreExpandOptions{MaxExpansion: 2})
	if err != nil || !res.Skipped || res.Reason != SkipTooCostly {
		t.Fatalf("expand=%+v err=%v", res, err)
	}
}

func TestLayeredStoreGlobalValueSetFallback(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: cs,
	})
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:global-vs","version":"1","compose":{"include":[{"system":"urn:global","concept":[{"code":"x"}]}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:global-vs", Version: "1", ResourceJSON: vsJSON,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PreExpandValueSet(ctx, m, GlobalScopeID, "urn:global-vs", "1", nil, PreExpandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	installs := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{
		{ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", Enabled: true},
		{ResourceType: "ValueSet", CanonicalURL: "urn:global-vs", Version: "1", Enabled: true},
	}}
	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = installs
	svc := NewLocalService(layered, "tenant-a")
	ex, err := svc.Expand(ctx, ExpandRequest{URL: "urn:global-vs"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "x" {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
}
