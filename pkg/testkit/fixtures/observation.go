package fixtures

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ObservationForPatient returns a vital-signs observation for the given patient id.
func ObservationForPatient(t *testing.T, patientID string) *types.ResourceEnvelope {
	t.Helper()
	if patientID == "" {
		t.Fatal("fixtures.ObservationForPatient: patient id is required")
	}
	data, err := json.Marshal(map[string]any{
		"resourceType": "Observation",
		"id":           "obs-1",
		"status":       "final",
		"code":         map[string]any{"text": "Body temperature"},
		"subject":      map[string]any{"reference": "Patient/" + patientID},
		"valueQuantity": map[string]any{
			"value": 37.2,
			"unit":  "Cel",
		},
	})
	if err != nil {
		t.Fatalf("fixtures.ObservationForPatient: %v", err)
	}
	return EnvelopeFromJSON(t, "Observation", data)
}

// ObservationVitals returns an observation referencing Patient/pat-jane.
func ObservationVitals(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return ObservationForPatient(t, "pat-jane")
}
