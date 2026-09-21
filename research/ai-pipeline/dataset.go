package aipipeline

import (
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

const (
	// PipelineVersion is the artefact version recorded in provenance bundles.
	PipelineVersion = "1.0.0"
	// TenantID is the synthetic tenant used by the demo.
	TenantID = "research-tenant"
	// ActorID is the clinician principal that is allowed to run the lab view.
	ActorID = "user-clinician"
	// DeniedActorID is a principal with no view permission.
	DeniedActorID = "user-denied"
	// ViewName is the registered lab observation view.
	ViewName = "lab_observations_view"
	// ViewVersion is pinned for provenance.
	ViewVersion = "1.0.0"
	// StubSeed is the deterministic model stub seed (GitHub issue 11).
	StubSeed int64 = 11
	// ConversationID is required by the executor for this demo.
	ConversationID = "research-ai-pipeline-11"
)

// FixedNow is the clock used by the reproducible pipeline.
func FixedNow() time.Time {
	return time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
}

// Dataset is the synthetic FHIR cohort used by the pipeline.
type Dataset struct {
	Patients     []*types.ResourceEnvelope
	Observations []*types.ResourceEnvelope
}

// LoadDataset parses the embedded synthetic resources.
func LoadDataset() (*Dataset, error) {
	codec := types.NewJSONCodec()
	ds := &Dataset{}
	for i, raw := range patientJSON() {
		env, err := codec.ParseJSON("Patient", raw)
		if err != nil {
			return nil, fmt.Errorf("patient[%d]: %w", i, err)
		}
		ds.Patients = append(ds.Patients, env)
	}
	for i, raw := range observationJSON() {
		env, err := codec.ParseJSON("Observation", raw)
		if err != nil {
			return nil, fmt.Errorf("observation[%d]: %w", i, err)
		}
		ds.Observations = append(ds.Observations, env)
	}
	return ds, nil
}

func patientJSON() [][]byte {
	return [][]byte{
		[]byte(`{"resourceType":"Patient","id":"pat-ada","identifier":[{"system":"http://haistack.dev/research/mrn","value":"ADA-001"}],"name":[{"family":"Cole","given":["Ada"]}],"gender":"female","birthDate":"1815-12-10"}`),
		[]byte(`{"resourceType":"Patient","id":"pat-grace","identifier":[{"system":"http://haistack.dev/research/mrn","value":"GRACE-001"}],"name":[{"family":"Hopper","given":["Grace"]}],"gender":"female","birthDate":"1906-12-09"}`),
		[]byte(`{"resourceType":"Patient","id":"pat-alan","identifier":[{"system":"http://haistack.dev/research/mrn","value":"ALAN-001"}],"name":[{"family":"Turing","given":["Alan"]}],"gender":"male","birthDate":"1912-06-23"}`),
	}
}

func observationJSON() [][]byte {
	return [][]byte{
		lab("obs-ada-hb", "pat-ada", "final", "718-7", "Hemoglobin", 13.4),
		lab("obs-ada-wbc", "pat-ada", "preliminary", "6690-2", "WBC", 6.1),
		lab("obs-grace-hb", "pat-grace", "final", "718-7", "Hemoglobin", 12.8),
		lab("obs-grace-na", "pat-grace", "final", "2951-2", "Sodium", 138),
		lab("obs-alan-hb", "pat-alan", "final", "718-7", "Hemoglobin", 14.1),
		lab("obs-alan-k", "pat-alan", "preliminary", "2823-3", "Potassium", 4.2),
	}
}

func lab(id, patient, status, code, display string, value float64) []byte {
	return []byte(fmt.Sprintf(`{
  "resourceType":"Observation",
  "id":%q,
  "status":%q,
  "code":{"text":%q,"coding":[{"system":"http://loinc.org","code":%q,"display":%q}]},
  "subject":{"reference":"Patient/%s"},
  "effectiveDateTime":"2026-09-01T09:00:00Z",
  "valueQuantity":{"value":%g,"unit":"1","system":"http://unitsofmeasure.org","code":"1"}
}`, id, status, display, code, display, patient, value))
}

// LabViewDefinition is the permissioned observation projection.
func LabViewDefinition() []byte {
	return []byte(`{
  "resourceType": "ViewDefinition",
  "name": "lab_observations_view",
  "version": "1.0.0",
  "status": "active",
  "description": "Final lab observations for the research AI pipeline",
  "resource": "Observation",
  "select": [{
    "column": [
      {"name": "id", "path": "Observation.id", "type": "string"},
      {"name": "patient", "path": "Observation.subject.reference", "type": "string"},
      {"name": "code", "path": "Observation.code.coding.first().code", "type": "string"},
      {"name": "display", "path": "Observation.code.text", "type": "string"},
      {"name": "value", "path": "Observation.value.ofType(Quantity).value", "type": "decimal"},
      {"name": "status", "path": "Observation.status", "type": "string"}
    ]
  }],
  "where": [
    {"path": "Observation.status = 'final'", "description": "Only final labs"}
  ],
  "permissions": ["read-lab-summary"]
}`)
}

// PolicyJSON is the deny-by-default policy that allows the lab view and run_view.
func PolicyJSON() []byte {
	return []byte(`{
  "version": "1",
  "rules": [
    {
      "name": "lab-view",
      "effect": "allow",
      "match": {
        "actions": ["execute-view"],
        "viewNames": ["lab_observations_view"],
        "anyPermissions": ["read-lab-summary"]
      },
      "reason": "clinicians may execute the lab observations view"
    },
    {
      "name": "ai-run-view",
      "effect": "allow",
      "match": {
        "actions": ["execute-ai-tool"],
        "toolNames": ["run_view"]
      },
      "reason": "AI agents may invoke run_view"
    },
    {
      "name": "observation-read",
      "effect": "allow",
      "match": {
        "actions": ["read"],
        "resourceTypes": ["Observation"],
        "anyPermissions": ["observation.read"]
      },
      "reason": "clinicians may read observations"
    }
  ]
}`)
}
