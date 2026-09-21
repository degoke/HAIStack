package http

import "testing"

func TestIsPublicFHIRPathExactMetadataRoute(t *testing.T) {
	t.Parallel()
	if !isPublicFHIRPath("/fhir", "/fhir/metadata") {
		t.Fatal("expected /fhir/metadata to be public")
	}
	if !isPublicFHIRPath("/fhir", "/fhir/metadata/") {
		t.Fatal("expected trailing slash metadata to be public")
	}
	if isPublicFHIRPath("/fhir", "/fhir/Patient/metadata") {
		t.Fatal("instance id metadata must not skip bearer auth")
	}
	if isPublicFHIRPath("/fhir", "/fhir/Patient") {
		t.Fatal("resource type must not be public")
	}
}
