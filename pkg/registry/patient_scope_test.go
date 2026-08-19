package registry_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

func TestSnapshotPatientSearchParameterCodeFromInstalledSearchParameters(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	for _, resourceType := range []string{"Patient", "Observation", "Appointment", "Practitioner"} {
		if err := manager.EnableResource(ctx, resourceType); err != nil {
			t.Fatalf("EnableResource %s: %v", resourceType, err)
		}
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}

	cases := map[string]string{
		"Observation": "patient",
		"Appointment": "patient",
	}
	for resourceType, want := range cases {
		got, ok := snapshot.PatientSearchParameterCode(resourceType)
		if !ok || got != want {
			t.Fatalf("%s PatientSearchParameterCode = (%q, %v), want (%q, true)", resourceType, got, ok, want)
		}
	}
	if _, ok := snapshot.PatientSearchParameterCode("Patient"); ok {
		t.Fatal("Patient resource type should not resolve a relationship parameter")
	}
	if _, ok := snapshot.PatientSearchParameterCode("Practitioner"); ok {
		t.Fatal("Practitioner should not resolve a patient scope parameter")
	}
}

func TestSnapshotPatientSearchParameterCodeDisabledResource(t *testing.T) {
	var snapshot *registry.Snapshot
	if _, ok := snapshot.PatientSearchParameterCode("Observation"); ok {
		t.Fatal("nil snapshot should not resolve patient scope parameter")
	}
}
