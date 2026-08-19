package fixtures

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// OfflinePatientCreate is a named preset for an offline-created patient.
func OfflinePatientCreate(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	return PatientJane(t)
}

// PatientJane returns a normalized Patient fixture (Jane Doe).
func PatientJane(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-jane",
		"gender": "female",
		"name": [{"given": ["Jane"], "family": "Doe"}],
		"telecom": [{"system": "phone", "value": "555-0100"}]
	}`)
	return EnvelopeFromJSON(t, "Patient", data)
}

// PatientJohn returns a normalized Patient fixture (John Smith).
func PatientJohn(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-john",
		"gender": "male",
		"name": [{"given": ["John"], "family": "Smith"}]
	}`)
	return EnvelopeFromJSON(t, "Patient", data)
}

// SamplePatient returns a minimal Patient envelope with the given id.
func SamplePatient(t *testing.T, id string) *types.ResourceEnvelope {
	t.Helper()
	if id == "" {
		t.Fatal("fixtures.SamplePatient: id is required")
	}
	data, err := json.Marshal(map[string]any{
		"resourceType": "Patient",
		"id":           id,
		"name":         []any{map[string]any{"family": "Sample"}},
	})
	if err != nil {
		t.Fatalf("fixtures.SamplePatient: %v", err)
	}
	return EnvelopeFromJSON(t, "Patient", data)
}
