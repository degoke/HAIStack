package terminology

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type memTerminologyInstallStore struct {
	rows []store.TerminologyInstallRecord
}

func (s *memTerminologyInstallStore) SetEnabled(_ context.Context, record store.TerminologyInstallRecord) error {
	s.rows = append(s.rows, record)
	return nil
}

func (s *memTerminologyInstallStore) UpsertInstall(ctx context.Context, record store.TerminologyInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *memTerminologyInstallStore) ListEnabled(_ context.Context) ([]store.TerminologyInstallRecord, error) {
	var out []store.TerminologyInstallRecord
	for _, row := range s.rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *memTerminologyInstallStore) ListInstalled(_ context.Context, filter store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	return store.FilterTerminologyInstalls(s.rows, filter), nil
}

func (s *memTerminologyInstallStore) Delete(_ context.Context, filter store.TerminologyInstallFilter) error {
	next := make([]store.TerminologyInstallRecord, 0, len(s.rows))
	for _, row := range s.rows {
		if filter.CanonicalURL != "" && row.CanonicalURL == filter.CanonicalURL {
			if filter.Version == "" || row.Version == filter.Version {
				if filter.ResourceType == "" || row.ResourceType == filter.ResourceType {
					continue
				}
			}
		}
		next = append(next, row)
	}
	s.rows = next
	return nil
}

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

	installs := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{{
		ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", Enabled: true,
	}}}
	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = installs
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

func TestLayeredStoreNilInstallsBlocksGlobal(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	svc := NewLocalService(layered, "tenant-a")
	got, err := svc.Lookup(ctx, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || got.Found {
		t.Fatalf("expected nil installs to block global, lookup=%+v err=%v", got, err)
	}
}

func TestLayeredStoreGlobalOptInRequired(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = &memTerminologyInstallStore{}
	svc := NewLocalService(layered, "tenant-a")
	got, err := svc.Lookup(ctx, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || got.Found {
		t.Fatalf("expected opt-in miss, lookup=%+v err=%v", got, err)
	}
}

func TestLayeredStoreDeleteDoesNotRemoveGlobal(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	if err := layered.DeleteResource(ctx, "tenant-a", "CodeSystem", "urn:global", "1"); err != nil {
		t.Fatal(err)
	}
	c, err := m.LookupConcept(ctx, GlobalScopeID, "urn:global", "1", "x")
	if err != nil || c == nil {
		t.Fatalf("global concept should remain: %+v err=%v", c, err)
	}
}

func TestLayeredStoreContextInstallsOverrideWired(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	wired := &memTerminologyInstallStore{}
	contextInstalls := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{{
		ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", Enabled: true,
	}}}
	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = wired
	svc := NewLocalService(layered, "tenant-a")

	got, err := svc.Lookup(ctx, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || got.Found {
		t.Fatalf("wired installs should block global, lookup=%+v err=%v", got, err)
	}

	ctx = store.ContextWithTerminologyInstalls(ctx, contextInstalls)
	c, err := layered.LookupConcept(ctx, "tenant-a", "urn:global", "1", "x")
	if err != nil || c == nil || c.Code != "x" {
		t.Fatalf("context installs should allow global, concept=%+v err=%v", c, err)
	}
}

func TestLocalServiceInvalidateCodeSystemClearsScopedLookupCache(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	svc := NewLocalService(layered, "tenant-a")
	ctxB := store.ContextWithTerminologyInstalls(ctx, &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{{
		ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", Enabled: true,
	}}})
	ctxB = store.ContextWithTerminologyScope(ctxB, "tenant-b")

	got, err := svc.Lookup(ctxB, LookupRequest{System: "urn:global", Code: "missing"})
	if err != nil || got.Found {
		t.Fatalf("expected negative cache entry, lookup=%+v err=%v", got, err)
	}
	svc.InvalidateCodeSystem("urn:global", "1")
	got, err = svc.Lookup(ctxB, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || !got.Found {
		t.Fatalf("expected cache invalidation to allow fresh lookup, lookup=%+v err=%v", got, err)
	}
}

func TestLocalServiceInvalidateValueSetClearsScopedExpandCache(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a","display":"A"}]}`)
	if err := Compile(ctx, m, "tenant-a", "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	vs := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"2","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	if err := Compile(ctx, m, "tenant-a", "", vs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "2", ResourceJSON: vs,
	})

	svc := NewLocalService(m, "tenant-a")
	ctxA := store.ContextWithTerminologyScope(ctx, "tenant-a")
	ex, err := svc.Expand(ctxA, ExpandRequest{URL: "urn:vs", Version: "2"})
	if err != nil || len(ex.Contains) != 1 {
		t.Fatalf("expand=%+v err=%v", ex, err)
	}
	if err := m.ReplaceValueSet(ctx, store.TerminologyValueSetRecord{
		ScopeID: "tenant-a", CanonicalURL: "urn:vs", Version: "2", ComposeJSON: `{"include":[{"system":"urn:cs","concept":[{"code":"b"}]}]}`,
	}, nil); err != nil {
		t.Fatal(err)
	}
	ex, err = svc.Expand(ctxA, ExpandRequest{URL: "urn:vs", Version: "2"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "a" {
		t.Fatalf("expected cached expansion, expand=%+v err=%v", ex, err)
	}
	svc.InvalidateValueSet("urn:vs", "2")
	ex, err = svc.Expand(ctxA, ExpandRequest{URL: "urn:vs", Version: "2"})
	if err != nil || len(ex.Contains) != 1 || ex.Contains[0].Code != "b" {
		t.Fatalf("expected fresh expansion after invalidation, expand=%+v err=%v", ex, err)
	}
}

func TestLocalServiceInvalidateValueSetClearsMemberLookupCache(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a","display":"A"}]}`)
	if err := Compile(ctx, m, "tenant-a", "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	vs := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"2","compose":{"include":[{"system":"urn:cs","concept":[{"code":"a"}]}]}}`)
	if err := Compile(ctx, m, "tenant-a", "", vs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: "tenant-a", ResourceType: "ValueSet", CanonicalURL: "urn:vs", Version: "2", ResourceJSON: vs,
	})

	svc := NewLocalService(m, "tenant-a")
	ctxA := store.ContextWithTerminologyScope(ctx, "tenant-a")
	got, err := svc.Lookup(ctxA, LookupRequest{System: "urn:cs", Version: "1", Code: "a"})
	if err != nil || !got.Found || got.Concept.Display != "A" {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	if err := m.ReplaceCodeSystem(ctx, "tenant-a", "urn:cs", "1", []store.TerminologyConceptRecord{{
		ScopeID: "tenant-a", SystemURL: "urn:cs", SystemVersion: "1", Code: "a", Display: "Alpha", Active: true,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Lookup(ctxA, LookupRequest{System: "urn:cs", Version: "1", Code: "a"})
	if err != nil || !got.Found || got.Concept.Display != "A" {
		t.Fatalf("expected cached lookup display A, got=%+v err=%v", got, err)
	}
	svc.InvalidateValueSet("urn:vs", "2")
	got, err = svc.Lookup(ctxA, LookupRequest{System: "urn:cs", Version: "1", Code: "a"})
	if err != nil || !got.Found || got.Concept.Display != "Alpha" {
		t.Fatalf("expected lookup refresh after ValueSet invalidation, got=%+v err=%v", got, err)
	}
}

func TestLocalServiceLookupCacheIsolatedPerScope(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	globalCS := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x","display":"Global"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", globalCS); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: globalCS,
	})

	layered := NewLayeredStore(m, "tenant-a")
	svc := NewLocalService(layered, "tenant-a")

	ctxB := store.ContextWithTerminologyInstalls(ctx, &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{{
		ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", Enabled: true,
	}}})
	ctxB = store.ContextWithTerminologyScope(ctxB, "tenant-b")
	got, err := svc.Lookup(ctxB, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || !got.Found {
		t.Fatalf("tenant-b lookup=%+v err=%v", got, err)
	}

	ctxA := store.ContextWithTerminologyScope(ctx, "tenant-a")
	got, err = svc.Lookup(ctxA, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || got.Found {
		t.Fatalf("tenant-a should miss without opt-in, lookup=%+v err=%v", got, err)
	}
}

func TestChainTenantUsesLayeredGlobalFallback(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:chain","version":"1","concept":[{"code":"a","display":"A"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:chain", Version: "1", ResourceJSON: cs,
	})
	installs := &memTerminologyInstallStore{rows: []store.TerminologyInstallRecord{{
		ResourceType: "CodeSystem", CanonicalURL: "urn:chain", Version: "1", Enabled: true,
	}}}
	layered := NewLayeredStore(m, "tenant-a")
	layered.Installs = installs
	tenant := NewLocalService(layered, "tenant-a")
	chain := Chain{Providers: []Provider{tenant}}
	got, err := chain.Lookup(ctx, LookupRequest{System: "urn:chain", Code: "a"})
	if err != nil || !got.Found {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
}
