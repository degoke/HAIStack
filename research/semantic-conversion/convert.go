package semanticconversion

import (
	"encoding/json"
	"fmt"
)

// ParticipantInformantSystem is the R5 provenance-participant-type code for
// Condition.asserter → Condition.participant.function. Gold testdata requires
// this system; the converter must emit it rather than minting gold from itself.
const ParticipantInformantSystem = "http://terminology.hl7.org/CodeSystem/provenance-participant-type"

// ParticipantInformantDisplay is the authored gold display for that coding.
const ParticipantInformantDisplay = "Informant"

// ObservationInterpretationSystem is the authored CodeSystem on promoted
// Observation.interpretation codings (R4 singleton → R5 list).
const ObservationInterpretationSystem = "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"

// ObservationInterpretationNDisplay is the authored display for interpretation code N.
const ObservationInterpretationNDisplay = "Normal"

// MedicationToySystem is the synthetic CodeSystem stamped onto wrapped
// MedicationRequest.medicationCodeableConcept codings (not RxNorm).
const MedicationToySystem = "http://haistack.dev/research/CodeSystem/toy-med"

// ConvertR4ToR5 applies the documented research remaps for corpus resource types.
// It is scored against authored gold R5 in testdata/corpus.json (the in-repo
// oracle, not a third-party mapping such as hl7.fhir.uv.xver). Structural
// scoring requires Convert output to equal gold except gold-only meta.source.
// These constraint URLs are emission values; authorship tests keep their own
// literals so testdata is not tied to this table.
func ConvertR4ToR5(resourceType string, r4 json.RawMessage) (json.RawMessage, []string, error) {
	var obj map[string]any
	if err := json.Unmarshal(r4, &obj); err != nil {
		return nil, nil, err
	}
	var loss []string
	switch resourceType {
	case "Patient":
		if _, ok := obj["animal"]; ok {
			delete(obj, "animal")
			loss = append(loss, "Patient.animal removed in R5")
		}
	case "Observation":
		if interp, ok := obj["interpretation"]; ok {
			if _, isList := interp.([]any); !isList {
				obj["interpretation"] = []any{stampInterpretation(interp)}
			}
		}
	case "Condition":
		if asserter, ok := obj["asserter"]; ok {
			delete(obj, "asserter")
			obj["participant"] = []any{map[string]any{
				"function": map[string]any{"coding": []any{map[string]any{
					"system":  ParticipantInformantSystem,
					"code":    "informant",
					"display": ParticipantInformantDisplay,
				}}},
				"actor": asserter,
			}}
		}
	case "MedicationRequest":
		if med, ok := obj["medicationCodeableConcept"]; ok {
			delete(obj, "medicationCodeableConcept")
			obj["medication"] = []any{map[string]any{"concept": stampMedicationConcept(med)}}
		}
		var reasons []any
		if rc, ok := obj["reasonCode"].([]any); ok {
			delete(obj, "reasonCode")
			for _, item := range rc {
				reasons = append(reasons, map[string]any{"concept": item})
			}
		}
		if rr, ok := obj["reasonReference"].([]any); ok {
			delete(obj, "reasonReference")
			for _, item := range rr {
				reasons = append(reasons, map[string]any{"reference": item})
			}
		}
		if len(reasons) > 0 {
			obj["reason"] = reasons
		}
		if reported, ok := obj["reportedReference"]; ok {
			delete(obj, "reportedReference")
			obj["reported"] = true
			obj["informationSource"] = []any{reported}
			loss = append(loss, "reportedReference collapsed to reported boolean plus informationSource")
		}
	default:
		return nil, nil, fmt.Errorf("unsupported resource type %s", resourceType)
	}
	out, err := json.Marshal(obj)
	return out, loss, err
}

func stampInterpretation(interp any) any {
	m, ok := interp.(map[string]any)
	if !ok {
		return interp
	}
	coding, _ := m["coding"].([]any)
	for i, item := range coding {
		cm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := cm["system"]; !ok {
			cm["system"] = ObservationInterpretationSystem
		}
		if code, _ := cm["code"].(string); code == "N" {
			if _, ok := cm["display"]; !ok {
				cm["display"] = ObservationInterpretationNDisplay
			}
		}
		coding[i] = cm
	}
	if coding != nil {
		m["coding"] = coding
	}
	return m
}

func stampMedicationConcept(med any) any {
	m, ok := med.(map[string]any)
	if !ok {
		return med
	}
	coding, _ := m["coding"].([]any)
	for i, item := range coding {
		cm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := cm["system"]; !ok {
			cm["system"] = MedicationToySystem
		}
		coding[i] = cm
	}
	if coding != nil {
		m["coding"] = coding
	}
	return m
}
