package auth_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type mapPatientResolver map[string]string

func (m mapPatientResolver) PatientIDForResource(_ context.Context, resourceType string, resource *types.ResourceEnvelope) (string, bool, error) {
	if resourceType == "Patient" {
		return resource.ID, true, nil
	}
	if id, ok := m[resource.ID]; ok {
		return id, true, nil
	}
	return "", false, nil
}

func TestCheckResourcePatientScope_ReadObservation(t *testing.T) {
	tenant := auth.TenantContext{TenantID: "t1", PatientScope: "pat-a"}
	resolver := mapPatientResolver{"obs-other": "pat-b"}

	obs := &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-other"}
	if err := auth.CheckEnvelopePatientScope(context.Background(), tenant, resolver, obs); err == nil {
		t.Fatal("expected deny for observation belonging to another patient")
	}

	obsOwn := &types.ResourceEnvelope{ResourceType: "Observation", ID: "obs-own"}
	resolver["obs-own"] = "pat-a"
	if err := auth.CheckEnvelopePatientScope(context.Background(), tenant, resolver, obsOwn); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestCheckResourcePatientScope_PatientRead(t *testing.T) {
	tenant := auth.TenantContext{TenantID: "t1", PatientScope: "pat-a"}
	resolver := mapPatientResolver{}

	if err := auth.CheckEnvelopePatientScope(context.Background(), tenant, resolver, &types.ResourceEnvelope{
		ResourceType: "Patient", ID: "pat-b",
	}); err == nil {
		t.Fatal("expected deny for other patient")
	}
}

func TestCompartmentPatientFromJSON(t *testing.T) {
	if got := auth.CompartmentPatientFromJSON("Observation", []byte(`{"subject":{"reference":"Patient/pat-2"}}`)); got != "pat-2" {
		t.Fatalf("Observation.subject = %q", got)
	}
	if got := auth.CompartmentPatientFromJSON("Appointment", []byte(`{"participant":[{"actor":{"reference":"Patient/pat-9"}}]}`)); got != "pat-9" {
		t.Fatalf("Appointment.participant.actor = %q", got)
	}
	if got := auth.OverlayPatientID("Patient", "pat-1", "pat-9"); got != "pat-1" {
		t.Fatalf("Patient.id = %q", got)
	}
	if got := auth.OverlayPatientID("Appointment", "a1", ""); got != "" {
		t.Fatalf("missing compartment must not use a stand-in, got %q", got)
	}
}

func TestCanReadResource_AppointmentCompartmentPatient(t *testing.T) {
	eng := mustEngine(t, baseConfig())
	scoped := auth.TenantContext{TenantID: "tenant-a", PatientScope: "pat-1", RoleBindings: []string{"clinician"}}
	own, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: scoped, ResourceType: "Appointment", ID: "a1", PatientID: "pat-1",
	})
	if err != nil || !own.Allowed {
		t.Fatalf("same compartment: %#v err=%v", own, err)
	}
	other, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: scoped, ResourceType: "Appointment", ID: "a2", PatientID: "pat-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.Allowed {
		t.Fatal("other compartment must deny")
	}
	unresolved, err := eng.CanReadResource(context.Background(), auth.ReadRequest{
		Principal: clinician(), Tenant: scoped, ResourceType: "Appointment", ID: "a1",
	})
	if err != nil || !unresolved.Allowed {
		t.Fatalf("empty PatientID remains unrestricted: %#v err=%v", unresolved, err)
	}
}
