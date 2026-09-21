package semanticconversion

import (
	"encoding/json"
	"fmt"
)

// ConvertR4ToR5 applies the documented research remaps for corpus resource types.
// It is a corpus converter, not a complete HL7 version conversion map.
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
				obj["interpretation"] = []any{interp}
			}
		}
	case "Condition":
		if asserter, ok := obj["asserter"]; ok {
			delete(obj, "asserter")
			obj["participant"] = []any{map[string]any{
				"function": map[string]any{"coding": []any{map[string]any{"code": "informant"}}},
				"actor":    asserter,
			}}
		}
	case "MedicationRequest":
		if med, ok := obj["medicationCodeableConcept"]; ok {
			delete(obj, "medicationCodeableConcept")
			obj["medication"] = []any{map[string]any{"concept": med}}
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
