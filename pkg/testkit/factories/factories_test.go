package factories_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/testkit/factories"
	"github.com/degoke/health-ai-stack/pkg/types"
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

func TestFactoriesSupportMetaAndPreserveTimestampPrecision(t *testing.T) {
	updated := time.Date(2024, 6, 1, 12, 0, 0, 123456789, time.UTC)
	patient, err := factories.NewPatient(
		factories.WithPatientMeta(map[string]any{"tag": []any{map[string]any{"code": "test"}}}),
		factories.WithVersionID("v1"),
		factories.WithLastUpdated(updated),
	)
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	meta, err := types.GetMeta(patient.JSON)
	if err != nil || meta == nil || meta.VersionID != "v1" || !meta.LastUpdated.Equal(updated) || !bytes.Contains(patient.JSON, []byte(`"tag"`)) {
		t.Fatalf("patient meta = %+v, %v", meta, err)
	}
	appointment, err := factories.NewAppointment(factories.WithAppointmentMeta(map[string]any{"tag": "a"}))
	if err != nil {
		t.Fatalf("NewAppointment: %v", err)
	}
	if meta, err := types.GetMeta(appointment.JSON); err != nil || meta == nil || !bytes.Contains(appointment.JSON, []byte(`"tag"`)) {
		t.Fatalf("appointment meta = %+v, %v", meta, err)
	}
	observation, err := factories.NewObservation(factories.WithObservationMeta(map[string]any{"tag": "o"}))
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	if meta, err := types.GetMeta(observation.JSON); err != nil || meta == nil || !bytes.Contains(observation.JSON, []byte(`"tag"`)) {
		t.Fatalf("observation meta = %+v, %v", meta, err)
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
