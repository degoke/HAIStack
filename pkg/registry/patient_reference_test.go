package registry_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestPatientReferenceResolver_ObservationSubject(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Observation"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	resolver := &registry.PatientReferenceResolver{Snapshot: snapshot, Engine: engine}
	env := &types.ResourceEnvelope{
		ResourceType: "Observation",
		ID:           "obs-1",
		JSON:         []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"coding":[{"code":"8867-4"}]},"subject":{"reference":"Patient/pat-1"}}`),
	}
	patientID, ok, err := resolver.PatientIDForResource(ctx, "Observation", env)
	if err != nil {
		t.Fatalf("PatientIDForResource: %v", err)
	}
	if !ok || patientID != "pat-1" {
		t.Fatalf("patientID = (%q, %v), want (pat-1, true)", patientID, ok)
	}
}
