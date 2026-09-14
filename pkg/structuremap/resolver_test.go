package structuremap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestResolveLatestVersionForUnversionedURL(t *testing.T) {
	url := "http://example.org/sdc/StructureMap/example-extraction"
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{}}
	addMap(store, "v1", Map{URL: url, Version: "1.0.0"})
	addMap(store, "v2", Map{URL: url, Version: "2.0.0"})
	resolver := &StoreResolver{Resources: store}
	m, err := resolver.Resolve(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "2.0.0" {
		t.Fatalf("expected latest version 2.0.0, got %#v", m)
	}
}

func TestResolveExactVersion(t *testing.T) {
	url := "http://example.org/sdc/StructureMap/example-extraction"
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{}}
	addMap(store, "v1", Map{URL: url, Version: "1.0.0"})
	addMap(store, "v2", Map{URL: url, Version: "2.0.0"})
	resolver := &StoreResolver{Resources: store}
	m, err := resolver.Resolve(context.Background(), url+"|1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %#v", m)
	}
}

func TestResolveAmbiguousDuplicateVersions(t *testing.T) {
	url := "http://example.org/sdc/StructureMap/example-extraction"
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{}}
	addMap(store, "a", Map{URL: url, Version: "1.0.0", Name: "A"})
	addMap(store, "b", Map{URL: url, Version: "1.0.0", Name: "B"})
	resolver := &StoreResolver{Resources: store}
	_, err := resolver.Resolve(context.Background(), url)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous version error, got %v", err)
	}
}

func TestStoreResolverRefreshesAfterCreate(t *testing.T) {
	url := "http://example.org/sdc/StructureMap/late-map"
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"StructureMap": {},
	}}
	resolver := &StoreResolver{Resources: store}
	_, err := resolver.Resolve(context.Background(), url)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing map, got %v", err)
	}
	addMap(store, "late", Map{URL: url, Version: "1.0.0"})
	m, err := resolver.Resolve(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != url {
		t.Fatalf("expected refreshed map, got %#v", m)
	}
}

func addMap(store *testResourceStore, id string, m Map) {
	raw, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	if store.byType["StructureMap"] == nil {
		store.byType["StructureMap"] = map[string]*types.ResourceEnvelope{}
	}
	store.byType["StructureMap"][id] = &types.ResourceEnvelope{
		ResourceType: "StructureMap",
		ID:           id,
		JSON:         raw,
	}
}
