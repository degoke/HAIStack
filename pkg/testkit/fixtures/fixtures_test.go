package fixtures_test

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
)

func TestPatientJaneStableEnvelope(t *testing.T) {
	a := fixtures.PatientJane(t)
	b := fixtures.PatientJane(t)
	if a.ID != "pat-jane" || a.ResourceType != "Patient" {
		t.Fatalf("envelope = %+v", a)
	}
	if a.Hash != b.Hash {
		t.Fatalf("hash not stable: %q vs %q", a.Hash, b.Hash)
	}
	if !json.Valid(a.JSON) {
		t.Fatal("JSON not valid")
	}
}

func TestAppointmentReferencesPatient(t *testing.T) {
	patient := fixtures.PatientJane(t)
	appt := fixtures.AppointmentForPatient(t, patient.ID)
	if appt.ResourceType != "Appointment" {
		t.Fatalf("type = %q", appt.ResourceType)
	}
	if !json.Valid(appt.JSON) {
		t.Fatal("JSON not valid")
	}
}

func TestObservationForPatient(t *testing.T) {
	obs := fixtures.ObservationVitals(t)
	if obs.ID != "obs-1" || obs.Hash == "" {
		t.Fatalf("obs = %+v", obs)
	}
}

func TestOfflinePatientCreatePreset(t *testing.T) {
	offline := fixtures.OfflinePatientCreate(t)
	jane := fixtures.PatientJane(t)
	if offline.Hash != jane.Hash {
		t.Fatalf("offline preset should match PatientJane")
	}
}
