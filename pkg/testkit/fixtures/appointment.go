package fixtures

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// AppointmentForPatient returns a booked appointment referencing the given patient id.
func AppointmentForPatient(t *testing.T, patientID string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Appointment",
		"id": "appt-1",
		"status": "booked",
		"description": "Annual checkup",
		"start": "2024-06-15T09:00:00Z",
		"participant": [{"actor": {"reference": "Patient/` + patientID + `"}, "status": "accepted"}]
	}`)
	return EnvelopeFromJSON(t, "Appointment", data)
}

// AppointmentBooked returns a booked appointment referencing Patient/pat-jane.
func AppointmentBooked(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return AppointmentForPatient(t, "pat-jane")
}
