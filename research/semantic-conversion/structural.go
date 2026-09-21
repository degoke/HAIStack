package semanticconversion

import "encoding/json"

// Authored constraint literals live here (not imported from convert.go) so
// testdata and the converter are not a single constant table.
const (
	constraintInformantSystem = "http://terminology.hl7.org/CodeSystem/provenance-participant-type"
	constraintInformantCode   = "informant"
	constraintInformantDisp   = "Informant"
	constraintInterpSystem    = "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"
	constraintInterpDisplay   = "Normal"
	constraintToyMedSystem    = "http://haistack.dev/research/CodeSystem/toy-med"
)

func structuralOK(pair Pair, got json.RawMessage) (bool, string, error) {
	switch pair.Category {
	case CategoryUnchanged:
		eq, err := jsonEqual(got, pair.R5)
		if err != nil {
			return false, "", err
		}
		if !eq {
			return false, "converted R5 does not match unchanged gold", nil
		}
		return true, "", nil
	case CategoryRemoved:
		if jsonHasPath(got, "animal") {
			return false, "converted R5 still has Patient.animal", nil
		}
		if jsonHasPath(pair.R5, "animal") {
			return false, "gold R5 still has Patient.animal", nil
		}
		return true, "", nil
	case CategoryRenamed:
		if !convertedHasInformant(got) {
			return false, "converted R5 missing informant participant", nil
		}
		return true, "", nil
	case CategoryCardinality:
		if !convertedHasInterpretationList(got) {
			return false, "converted R5 missing list interpretation with v3 system/display", nil
		}
		return true, "", nil
	case CategoryCodeableConcept, CategoryTypeChange:
		if !convertedHasMedicationConcept(got) {
			return false, "converted R5 missing medication CodeableReference", nil
		}
		if pair.Category == CategoryTypeChange && !jsonHasPath(got, "reported") {
			return false, "converted R5 missing reported boolean", nil
		}
		return true, "", nil
	default:
		return false, "unknown category " + pair.Category, nil
	}
}

func goldHasAuthoredSource(raw json.RawMessage, spec string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	meta, _ := obj["meta"].(map[string]any)
	src, _ := meta["source"].(string)
	return src == spec
}

func jsonHasPath(raw json.RawMessage, key string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	_, ok := obj[key]
	return ok
}

func convertedHasInformant(raw json.RawMessage) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	parts, _ := obj["participant"].([]any)
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		fn, _ := pm["function"].(map[string]any)
		for _, c := range codingList(fn) {
			if str(c["system"]) == constraintInformantSystem &&
				str(c["code"]) == constraintInformantCode &&
				str(c["display"]) == constraintInformantDisp {
				return true
			}
		}
	}
	return false
}

func convertedHasInterpretationList(raw json.RawMessage) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	list, ok := obj["interpretation"].([]any)
	if !ok || len(list) == 0 {
		return false
	}
	item, _ := list[0].(map[string]any)
	for _, c := range codingList(item) {
		if str(c["system"]) == constraintInterpSystem && str(c["display"]) == constraintInterpDisplay {
			return true
		}
	}
	return false
}

func convertedHasMedicationConcept(raw json.RawMessage) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	meds, _ := obj["medication"].([]any)
	if len(meds) == 0 {
		return false
	}
	m, _ := meds[0].(map[string]any)
	concept, _ := m["concept"].(map[string]any)
	if concept == nil {
		return false
	}
	codings := codingList(concept)
	if len(codings) == 0 {
		return true
	}
	for _, c := range codings {
		if str(c["system"]) == constraintToyMedSystem {
			return true
		}
	}
	return false
}

func codingList(obj map[string]any) []map[string]any {
	if obj == nil {
		return nil
	}
	raw, _ := obj["coding"].([]any)
	var out []map[string]any
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
