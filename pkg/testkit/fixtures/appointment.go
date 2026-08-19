package fixtures

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// AppointmentForPatient returns a booked appointment referencing the given patient id.
func AppointmentForPatient(t *testing.T, patientID string) *types.ResourceEnvelope {
	t.Helper()
	if patientID == "" {
		t.Fatal("fixtures.AppointmentForPatient: patient id is required")
	}
	data, err := json.Marshal(map[string]any{
		"resourceType": "Appointment",
		"id":           "appt-1",
		"status":       "booked",
		"description":  "Annual checkup",
		"start":        "2024-06-15T09:00:00Z",
		"participant": []any{map[string]any{
			"actor":  map[string]any{"reference": "Patient/" + patientID},
			"status": "accepted",
		}},
	})
	if err != nil {
		t.Fatalf("fixtures.AppointmentForPatient: %v", err)
	}
	return EnvelopeFromJSON(t, "Appointment", data)
}

// AppointmentBooked returns a booked appointment referencing Patient/pat-jane.
func AppointmentBooked(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return AppointmentForPatient(t, "pat-jane")
}
