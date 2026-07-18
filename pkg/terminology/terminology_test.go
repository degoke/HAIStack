package terminology

import (
	"context"
	"github.com/degoke/health-ai-stack/pkg/store"
	"testing"
)

func TestLocalCodeSystemAndValueSet(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:test","version":"2","concept":[{"code":"a","display":"Alpha"},{"code":"b","display":"Beta"}]}`)
	if err := m.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "CodeSystem", CanonicalURL: "urn:test", Version: "2", ResourceJSON: cs}); err != nil {
		t.Fatal(err)
	}
	if err := Compile(ctx, m, "s", "", cs); err != nil {
		t.Fatal(err)
	}
	svc := &LocalService{Store: m, ScopeID: "s"}
	got, err := svc.Lookup(ctx, LookupRequest{System: "urn:test", Code: "a"})
	if err != nil || !got.Found || got.Concept.Display != "Alpha" {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	v := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","compose":{"include":[{"system":"urn:test","concept":[{"code":"a"}]}]}}`)
	if err := m.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "1", ResourceJSON: v}); err != nil {
		t.Fatal(err)
	}
	if err := Compile(ctx, m, "s", "", v); err != nil {
		t.Fatal(err)
	}
	ex, err := svc.Expand(ctx, ExpandRequest{URL: "urn:vs"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "a" {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
	bad, err := svc.ValidateCode(ctx, ValidateCodeRequest{Coding: Coding{System: "urn:test", Code: "a", Display: "wrong"}})
	if err != nil || bad.Status != Valid || !bad.DisplayWarning {
		t.Fatalf("validation=%+v err=%v", bad, err)
	}
}

func TestRetiredCurrentVersionExcluded(t *testing.T) {
	m := NewMemoryStore()
	ctx := context.Background()
	for _, v := range []string{"1", "2"} {
		r := []byte(`{"resourceType":"CodeSystem","url":"urn:x","version":"` + v + `","concept":[{"code":"c"}]}`)
		_ = m.PutResource(ctx, store.TerminologyResourceRecord{ScopeID: "s", ResourceType: "CodeSystem", CanonicalURL: "urn:x", Version: v, Status: map[string]string{"1": "retired", "2": "active"}[v], ResourceJSON: r})
		_ = Compile(ctx, m, "s", "", r)
	}
	c, _ := m.LookupConcept(ctx, "s", "urn:x", "", "c")
	if c == nil || c.SystemVersion != "2" {
		t.Fatalf("current=%+v", c)
	}
}
