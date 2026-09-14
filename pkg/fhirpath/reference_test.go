package fhirpath_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestParseReferenceForRead_AbsoluteURL(t *testing.T) {
	resourceType, id, ok := fhirpath.ParseReferenceForRead("https://example.com/fhir/Patient/pat-1", "https://example.com/fhir")
	if !ok {
		t.Fatal("expected absolute URL to parse")
	}
	if resourceType != "Patient" || id != "pat-1" {
		t.Fatalf("got %s/%s", resourceType, id)
	}
}

func TestParseReferenceForRead_URN(t *testing.T) {
	_, id, ok := fhirpath.ParseReferenceForRead("urn:uuid:550e8400-e29b-41d4-a716-446655440000", "")
	if !ok || id != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("urn parse ok=%v id=%q", ok, id)
	}
}

func TestParseReferenceForRead_TypedRelative(t *testing.T) {
	resourceType, id, ok := fhirpath.ParseReferenceForRead("Observation/obs-1", "")
	if !ok || resourceType != "Observation" || id != "obs-1" {
		t.Fatalf("typed parse = %s/%s ok=%v", resourceType, id, ok)
	}
}

func TestEnhancedResourceStoreResolver_AbsoluteURL(t *testing.T) {
	resolver := fhirpath.EnhancedResourceStoreResolver(fhirpath.ResourceResolverConfig{
		BaseURL: "https://example.com/fhir",
		Read: func(_ context.Context, resourceType, id string) (any, error) {
			return resourceType + "/" + id, nil
		},
	})
	got, err := resolver(context.Background(), "https://example.com/fhir/Patient/pat-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "Patient/pat-1" {
		t.Fatalf("resolved = %v", got)
	}
}

func TestEnhancedResourceStoreResolver_ContainedFragment(t *testing.T) {
	parent := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"contained": []any{
			map[string]any{"resourceType": "Observation", "id": "obs-inline", "status": "final"},
		},
	}
	ctx := fhirpath.WithEvaluationResource(context.Background(), parent)
	resolver := fhirpath.EnhancedResourceStoreResolver(fhirpath.ResourceResolverConfig{})
	got, err := resolver(ctx, "#obs-inline")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	obs, ok := got.(*types.ResourceEnvelope)
	if !ok || obs.ID != "obs-inline" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestEvalResolveContainedFragmentReference(t *testing.T) {
	ctx := context.Background()
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "obs-1",
		"status": "final",
		"code": {"text": "Heart rate"},
		"contained": [{
			"resourceType": "Patient",
			"id": "p-inline",
			"name": [{"family": "Inline"}]
		}],
		"subject": {"reference": "#p-inline"}
	}`))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	eng := newEngine(t, fhirpath.Config{
		Resolve: fhirpath.EnhancedResourceStoreResolver(fhirpath.ResourceResolverConfig{}),
	})
	values, err := eng.Eval(ctx, "Observation.subject.resolve().name.family", env)
	if err != nil {
		t.Fatalf("Eval resolve contained: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("len(values) = %d, want 1", len(values))
	}
	s, err := values[0].String()
	if err != nil || s != "Inline" {
		t.Fatalf("resolved family = %q, %v, want Inline", s, err)
	}
}
