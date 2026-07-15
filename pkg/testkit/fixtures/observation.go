package fixtures

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ObservationForPatient returns a vital-signs observation for the given patient id.
func ObservationForPatient(t *testing.T, patientID string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Observation",
		"id": "obs-1",
		"status": "final",
		"code": {"text": "Body temperature"},
		"subject": {"reference": "Patient/` + patientID + `"},
		"valueQuantity": {"value": 37.2, "unit": "Cel"}
	}`)
	return EnvelopeFromJSON(t, "Observation", data)
}

// ObservationVitals returns an observation referencing Patient/pat-jane.
func ObservationVitals(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return ObservationForPatient(t, "pat-jane")
}
