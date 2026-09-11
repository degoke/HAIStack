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
	res1, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:vs", "1", nil, PreExpandOptions{})
	if err != nil || res1.Skipped || res1.Members != 1 {
		t.Fatalf("first expand=%+v err=%v", res1, err)
	}
	res2, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:vs", "1", nil, PreExpandOptions{})
	if err != nil || !res2.Skipped || res2.Reason != SkipAlreadyExpanded {
		t.Fatalf("second expand=%+v err=%v", res2, err)
	}
}

func TestPreExpandPersistsLegacyServerExpansion(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","compose":{"include":[{"system":"urn:cs"}]},"expansion":{"contains":[{"system":"urn:cs","code":"a","display":"A"}]}}`)
	compose := `{"include":[{"system":"urn:cs"}]}`
	if err := m.ReplaceValueSet(ctx, store.TerminologyValueSetRecord{
		ScopeID: GlobalScopeID, CanonicalURL: "urn:vs", Version: "1", ComposeJSON: compose, ExpansionFingerprint: ComposeFingerprint(compose),
	}, nil); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "1", ResourceJSON: vsJSON,
	})
	res, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:vs", "1", vsJSON, PreExpandOptions{})
	if err != nil || res.Skipped || res.Members != 1 {
		t.Fatalf("expand=%+v err=%v", res, err)
	}
	res2, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:vs", "1", vsJSON, PreExpandOptions{})
	if err != nil || !res2.Skipped || res2.Reason != SkipServerExpansion {
		t.Fatalf("second expand=%+v err=%v", res2, err)
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
	res, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:big-vs", "1", nil, PreExpandOptions{MaxExpansion: 2})
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
	_, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:global-vs", "1", nil, PreExpandOptions{})
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

func TestLayeredStoreOverlayValueSetDoesNotUseGlobalMembers(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"g"},{"code":"t"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	globalVS := []byte(`{"resourceType":"ValueSet","url":"urn:shared","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"g"}]}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:shared", Version: "1",
		ResourceJSON: globalVS,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:shared", "1", nil, PreExpandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tenantVS := []byte(`{"resourceType":"ValueSet","url":"urn:shared","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"t"}]}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "ValueSet", CanonicalURL: "urn:shared", Version: "1",
		ResourceJSON: tenantVS,
	}); err != nil {
		t.Fatal(err)
	}
	installs := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{
		{ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", Enabled: true},
		{ResourceType: "ValueSet", CanonicalURL: "urn:shared", Version: "1", Enabled: true},
	}}
	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = installs
	svc := NewLocalService(layered, "tenant-a")
	ex, err := svc.Expand(ctx, ExpandRequest{URL: "urn:shared"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "t" {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
}

func TestShouldEnqueuePreExpand(t *testing.T) {
	finite := []byte(`{"resourceType":"ValueSet","url":"urn:vs","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	if !ShouldEnqueuePreExpand(finite) {
		t.Fatal("expected finite compose to enqueue")
	}
	fullCS := []byte(`{"resourceType":"ValueSet","url":"urn:vs","compose":{"include":[{"system":"urn:cs"}]}}`)
	if ShouldEnqueuePreExpand(fullCS) {
		t.Fatal("expected full CodeSystem include to skip enqueue")
	}
	valueSetOnly := []byte(`{"resourceType":"ValueSet","url":"urn:vs","compose":{"include":[{"valueSet":["urn:other"]}]}}`)
	if ShouldEnqueuePreExpand(valueSetOnly) {
		t.Fatal("expected valueSet-only include to skip enqueue")
	}
	withExpansion := []byte(`{"resourceType":"ValueSet","url":"urn:vs","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]},"expansion":{"contains":[{"system":"urn:cs","code":"a"}]}}`)
	if ShouldEnqueuePreExpand(withExpansion) {
		t.Fatal("expected server expansion to skip enqueue")
	}
}

func TestExpandIgnoresStaleMembersWhenComposeChanges(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a"},{"code":"b"}]}`)
	if err := Compile(ctx, m, "tenant-a", "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	oldCompose := `{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}`
	if err := m.ReplaceValueSet(ctx, store.TerminologyValueSetRecord{
		ScopeID: "tenant-a", CanonicalURL: "urn:vs", Version: "1", ComposeJSON: oldCompose, ExpansionFingerprint: ComposeFingerprint(oldCompose),
	}, []store.TerminologyExpansionMemberRecord{{ScopeID: "tenant-a", SystemURL: "urn:cs", Code: "a"}}); err != nil {
		t.Fatal(err)
	}
	newCompose := `{"include":[{"system":"urn:cs","concept":[{"code":"b"}]}]}`
	if err := m.ReplaceValueSet(ctx, store.TerminologyValueSetRecord{
		ScopeID: "tenant-a", CanonicalURL: "urn:vs", Version: "1", ComposeJSON: newCompose, ExpansionFingerprint: ComposeFingerprint(oldCompose),
	}, []store.TerminologyExpansionMemberRecord{{ScopeID: "tenant-a", SystemURL: "urn:cs", Code: "a"}}); err != nil {
		t.Fatal(err)
	}
	svc := NewLocalService(m, "tenant-a")
	ex, err := svc.Expand(ctx, ExpandRequest{URL: "urn:vs"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "b" {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
}

func TestPreExpandScopeListsScopeOnly(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalVS := []byte(`{"resourceType":"ValueSet","url":"urn:global-only","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	if err := Install(ctx, m, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:global-only", Version: "1", ResourceJSON: globalVS,
	}); err != nil {
		t.Fatal(err)
	}
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	_, err := PreExpandValueSet(ctx, m, m, GlobalScopeID, "urn:global-only", "1", nil, PreExpandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	installs := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{
		{ResourceType: "ValueSet", CanonicalURL: "urn:global-only", Version: "1", Enabled: true},
	}}
	composeStore := StoreForScope(m, "tenant-a", installs, "tenant-a")
	results, err := PreExpandScope(ctx, m, composeStore, "tenant-a", nil, PreExpandOptions{Installs: installs})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("tenant scope should not list global VS, got %+v", results)
	}
	members, err := m.ListValueSetMembers(ctx, "tenant-a", "urn:global-only", "1")
	if err != nil || len(members) != 0 {
		t.Fatalf("tenant should not get global expansion copy: members=%d err=%v", len(members), err)
	}
}

func TestEligiblePackPreExpandURLsRequiresPackVersion(t *testing.T) {
	ctx := context.Background()
	defs := &memDefinitionStore{}
	_, err := EligiblePackPreExpandURLs(ctx, defs, NewMemoryStore(), GlobalScopeID, "pack-a", "")
	if err == nil {
		t.Fatal("expected packVersion required error")
	}
}

func TestPreExpandPackFiltersEligibleValueSets(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	defs := &memDefinitionStore{}
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a"}]}`)
	finiteVS := []byte(`{"resourceType":"ValueSet","url":"urn:finite","version":"1","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	fullCSVS := []byte(`{"resourceType":"ValueSet","url":"urn:full","version":"1","compose":{"include":[{"system":"urn:cs"}]}}`)
	for _, raw := range [][]byte{cs, finiteVS, fullCSVS} {
		if err := Install(ctx, m, store.TerminologyResourceRecord{
			ScopeID: GlobalScopeID, ResourceType: resourceTypeFromJSON(raw), CanonicalURL: urlFromJSON(raw), Version: "1", ResourceJSON: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{CanonicalURL: "urn:finite", Version: "1", FHIRResourceType: "ValueSet", PackageName: "pack-a", PackageVersion: "1.0", JSONData: finiteVS}, nil)
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{CanonicalURL: "urn:full", Version: "1", FHIRResourceType: "ValueSet", PackageName: "pack-a", PackageVersion: "1.0", JSONData: fullCSVS}, nil)
	results, err := PreExpandPack(ctx, m, m, defs, GlobalScopeID, "pack-a", "1.0", PreExpandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "urn:finite" || results[0].Skipped {
		t.Fatalf("results=%+v", results)
	}
}

func resourceTypeFromJSON(raw []byte) string {
	var r struct {
		ResourceType string `json:"resourceType"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.ResourceType
}

func urlFromJSON(raw []byte) string {
	var r struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.URL
}
