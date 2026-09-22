package cql

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/degoke/haistack/pkg/types"
)

type testResourceStore struct {
	byType map[string]map[string]*types.ResourceEnvelope
	reads  int
}

func (s *testResourceStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (s *testResourceStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (s *testResourceStore) Delete(context.Context, string, string) error          { return nil }
func (s *testResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	_, ok := s.byType[resourceType][id]
	return ok, nil
}
func (s *testResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.reads++
	if env, ok := s.byType[resourceType][id]; ok {
		return env, nil
	}
	return nil, context.Canceled
}
func (s *testResourceStore) ListIDs(_ context.Context, resourceType string, _, _ int) ([]string, error) {
	byID := s.byType[resourceType]
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	return ids, nil
}

func libraryEnvelope(t *testing.T, id, url, name, version, src string) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON("Library", []byte(`{
		"resourceType": "Library",
		"id": "`+id+`",
		"url": "`+url+`",
		"name": "`+name+`",
		"version": "`+version+`",
		"status": "active",
		"content": [{"contentType": "text/cql", "data": "`+base64.StdEncoding.EncodeToString([]byte(src))+`"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestStoreLibraryResolverCompilesOnlyMatchingCandidates(t *testing.T) {
	good := libraryEnvelope(t, "good", "http://example.org/Library/Good", "Good", "1.0.0",
		"library Good version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": true\n")
	other := libraryEnvelope(t, "other", "http://example.org/Library/Other", "Other", "1.0.0",
		"library Other version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"Y\": 1\n")
	broken := libraryEnvelope(t, "broken", "http://example.org/Library/Broken", "Broken", "1.0.0",
		"this is not cql")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {
			"good":   good,
			"other":  other,
			"broken": broken,
		},
	}}
	eng := testEngine(t)
	r := &StoreLibraryResolver{Resources: store, Engine: eng}
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/Good")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Name != "Good" {
		t.Fatalf("resolved: %#v", lib)
	}
	if r.compileCount != 1 {
		t.Fatalf("expected one compile of the matching library, got %d", r.compileCount)
	}
	lib2, err := r.Resolve(context.Background(), "http://example.org/Library/Good")
	if err != nil {
		t.Fatal(err)
	}
	if lib2 != lib {
		t.Fatal("expected cached library pointer")
	}
	if r.compileCount != 1 {
		t.Fatalf("cache hit must not recompile, got %d", r.compileCount)
	}
}

func TestStoreLibraryResolverIgnoresUnrelatedBrokenLibrary(t *testing.T) {
	good := libraryEnvelope(t, "good", "http://example.org/Library/Good", "Good", "1.0.0",
		"library Good version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": true\n")
	broken := libraryEnvelope(t, "broken", "http://example.org/Library/Broken", "Broken", "1.0.0",
		"this is not cql")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"good": good, "broken": broken},
	}}
	r := &StoreLibraryResolver{Resources: store, Engine: testEngine(t)}
	if _, err := r.Resolve(context.Background(), "http://example.org/Library/Good"); err != nil {
		t.Fatal(err)
	}
	_, err := r.Resolve(context.Background(), "http://example.org/Library/Broken")
	if err == nil {
		t.Fatal("expected compile error for matching broken library")
	}
	if errors.Is(err, ErrLibraryNotFound) {
		t.Fatalf("matching candidate should surface compile error, got %v", err)
	}
}

func TestStoreLibraryResolverCacheInvalidatesOnHashChange(t *testing.T) {
	first := libraryEnvelope(t, "lib", "http://example.org/Library/V", "V", "1.0.0",
		"library V version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 1\n")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"lib": first},
	}}
	r := &StoreLibraryResolver{Resources: store, Engine: testEngine(t)}
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/V")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Engine.EvalDefine(context.Background(), lib, "X", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("initial define: %#v", got)
	}
	updated := libraryEnvelope(t, "lib", "http://example.org/Library/V", "V", "1.0.0",
		"library V version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 2\n")
	store.byType["Library"]["lib"] = updated
	lib2, err := r.Resolve(context.Background(), "http://example.org/Library/V")
	if err != nil {
		t.Fatal(err)
	}
	if r.compileCount != 2 {
		t.Fatalf("expected recompile after hash change, got %d", r.compileCount)
	}
	got, err = r.Engine.EvalDefine(context.Background(), lib2, "X", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("updated define: %#v", got)
	}
}

func TestStoreLibraryResolverSkipsBrokenSiblingVersion(t *testing.T) {
	broken := libraryEnvelope(t, "v1", "http://example.org/Library/Good", "Good", "1.0.0",
		"this is not cql")
	good := libraryEnvelope(t, "v2", "http://example.org/Library/Good", "Good", "2.0.0",
		"library Good version '2.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": true\n")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"v1": broken, "v2": good},
	}}
	r := &StoreLibraryResolver{Resources: store, Engine: testEngine(t)}
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/Good")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Version != "2.0.0" {
		t.Fatalf("expected latest good version, got %s", lib.Version)
	}
}

func TestStoreLibraryResolverPicksSemverLatest(t *testing.T) {
	older := libraryEnvelope(t, "a", "http://example.org/Library/V", "V", "1.9.0",
		"library V version '1.9.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 9\n")
	newer := libraryEnvelope(t, "b", "http://example.org/Library/V", "V", "1.10.0",
		"library V version '1.10.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 10\n")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"a": older, "b": newer},
	}}
	r := &StoreLibraryResolver{Resources: store, Engine: testEngine(t)}
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/V")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Version != "1.10.0" {
		t.Fatalf("semver latest: got %s", lib.Version)
	}
}

func TestStoreLibraryResolverSeesRetargetedURL(t *testing.T) {
	good := libraryEnvelope(t, "good", "http://example.org/Library/Good", "Good", "1.0.0",
		"library Good version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 1\n")
	other := libraryEnvelope(t, "other", "http://example.org/Library/Other", "Other", "1.0.0",
		"library Other version '1.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"Y\": 1\n")
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"good": good, "other": other},
	}}
	r := &StoreLibraryResolver{Resources: store, Engine: testEngine(t)}
	if _, err := r.Resolve(context.Background(), "http://example.org/Library/Good"); err != nil {
		t.Fatal(err)
	}
	retargeted := libraryEnvelope(t, "other", "http://example.org/Library/Good", "Good", "2.0.0",
		"library Good version '2.0.0'\nusing FHIR version '4.0.1'\ncontext Patient\ndefine \"X\": 2\n")
	store.byType["Library"]["other"] = retargeted
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/Good")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Version != "2.0.0" {
		t.Fatalf("expected retargeted 2.0.0, got %s", lib.Version)
	}
}

func TestCompareVersionNumericOrder(t *testing.T) {
	if compareVersion("1.10.0", "1.9.0") <= 0 {
		t.Fatal("1.10.0 should be newer than 1.9.0")
	}
	if compareVersion("2.0.0", "1.99.0") <= 0 {
		t.Fatal("2.0.0 should be newer than 1.99.0")
	}
	if compareVersion("1.0.0", "1.0.0-beta") <= 0 {
		t.Fatal("release should be newer than prerelease")
	}
}
