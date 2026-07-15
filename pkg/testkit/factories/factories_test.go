package factories_test

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/factories"
)

func TestNewPatientDefaults(t *testing.T) {
	env, err := factories.NewPatient()
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	if env.ResourceType != "Patient" || env.ID != "pat-1" || env.Hash == "" {
		t.Fatalf("envelope = %+v", env)
	}
	if !json.Valid(env.JSON) {
		t.Fatal("invalid JSON")
	}
}

func TestNewPatientOptions(t *testing.T) {
	env, err := factories.NewPatient(
		factories.WithPatientID("custom"),
		factories.WithFamilyName("Smith"),
		factories.WithTelecom("555-9999"),
	)
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	if env.ID != "custom" {
		t.Fatalf("id = %q", env.ID)
	}
}

func TestNewPatientInvalidID(t *testing.T) {
	if _, err := factories.NewPatient(factories.WithPatientID("")); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewAppointmentRequiresPatientRef(t *testing.T) {
	if _, err := factories.NewAppointment(factories.WithPatientReference("")); err == nil {
		t.Fatal("expected error for empty patient ref")
	}
	appt, err := factories.NewAppointment(factories.WithPatientReference("pat-jane"))
	if err != nil {
		t.Fatalf("NewAppointment: %v", err)
	}
	if appt.ResourceType != "Appointment" {
		t.Fatalf("type = %q", appt.ResourceType)
	}
}

func TestNewObservation(t *testing.T) {
	obs, err := factories.NewObservation(factories.WithSubjectReference("pat-1"))
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	if obs.ResourceType != "Observation" || obs.Hash == "" {
		t.Fatalf("obs = %+v", obs)
	}
}
