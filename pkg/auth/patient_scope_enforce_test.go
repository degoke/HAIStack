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
