package fhirpath_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
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
