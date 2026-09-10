package terminology

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type stubRemote struct {
	lookupCalls int
	expandCalls int
}

func (s *stubRemote) Lookup(context.Context, LookupRequest) (*LookupResult, error) {
	s.lookupCalls++
	return &LookupResult{Found: true, Concept: Concept{Code: "remote"}}, nil
}

func (s *stubRemote) Expand(context.Context, ExpandRequest) (*Expansion, error) {
	s.expandCalls++
	return &Expansion{Total: 1, Contains: []Coding{{Code: "remote"}}}, nil
}

func (s *stubRemote) ValidateCode(context.Context, ValidateCodeRequest) (*ValidationResult, error) {
	return &ValidationResult{Status: Valid}, nil
}

func TestOptInRemoteGateBlocksGlobalWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: cs,
	})

	remote := &stubRemote{}
	gate := NewOptInRemoteGate(remote, m, &memTerminologyInstallStore{})
	got, err := gate.Lookup(ctx, LookupRequest{System: "urn:global", Code: "x"})
	if err != nil || got.Found || remote.lookupCalls != 0 {
		t.Fatalf("lookup=%+v err=%v remoteCalls=%d", got, err, remote.lookupCalls)
	}
}

func TestOptInRemoteGateBlocksExpandForGlobalValueSetWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:global","version":"1","concept":[{"code":"x"}]}`)
	if err := Compile(ctx, m, GlobalScopeID, "", cs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:global", Version: "1", ResourceJSON: cs,
	})
	vs := []byte(`{"resourceType":"ValueSet","url":"urn:global-vs","version":"1","compose":{"include":[{"system":"urn:global","concept":[{"code":"x"}]}]}}`)
	if err := Compile(ctx, m, GlobalScopeID, "", vs); err != nil {
		t.Fatal(err)
	}
	_ = m.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "ValueSet", CanonicalURL: "urn:global-vs", Version: "1", ResourceJSON: vs,
	})

	remote := &stubRemote{}
	gate := NewOptInRemoteGate(remote, m, &memTerminologyInstallStore{})
	ex, err := gate.Expand(ctx, ExpandRequest{URL: "urn:global-vs"})
	if err != ErrExpansionNotFound || ex != nil || remote.expandCalls != 0 {
		t.Fatalf("expand=%+v err=%v remoteCalls=%d", ex, err, remote.expandCalls)
	}
}

func TestOptInRemoteGateAllowsNonGlobalSystems(t *testing.T) {
	ctx := context.Background()
	remote := &stubRemote{}
	gate := NewOptInRemoteGate(remote, NewMemoryStore(), &memTerminologyInstallStore{})
	got, err := gate.Lookup(ctx, LookupRequest{System: "http://snomed.info/sct", Code: "x"})
	if err != nil || !got.Found || remote.lookupCalls != 1 {
		t.Fatalf("lookup=%+v err=%v remoteCalls=%d", got, err, remote.lookupCalls)
	}
}
