package semanticconversion

import (
	"encoding/json"
	"fmt"
)

// Category classifies a conversion pair.
const (
	CategoryUnchanged       = "unchanged"
	CategoryRenamed         = "renamed"
	CategoryCardinality     = "cardinality"
	CategoryTypeChange      = "type-change"
	CategoryCodeableConcept = "codeableconcept"
)

// Assertion is a FHIRPath check on R4 and/or R5 instances.
type Assertion struct {
	Name string   `json:"name"`
	R4   string   `json:"r4,omitempty"`
	R5   string   `json:"r5,omitempty"`
	Want []string `json:"want,omitempty"`
}

// Pair is one R4 input and expected R5 output with scoring metadata.
type Pair struct {
	ID              string          `json:"id"`
	ResourceType    string          `json:"resourceType"`
	Category        string          `json:"category"`
	R4              json.RawMessage `json:"r4"`
	R5              json.RawMessage `json:"r5"`
	Assertions      []Assertion     `json:"assertions"`
	InformationLoss []string        `json:"informationLoss,omitempty"`
}

// Corpus returns at least 50 synthetic R4/R5 pairs.
func Corpus() []Pair {
	out := make([]Pair, 0, 60)
	out = append(out, patientPairs()...)
	out = append(out, observationPairs()...)
	out = append(out, conditionPairs()...)
	out = append(out, medicationRequestPairs()...)
	return out
}

func patientPairs() []Pair {
	var out []Pair
	genders := []string{"female", "male", "other", "unknown"}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("pat-%02d", i+1)
		gender := genders[i%len(genders)]
		r4 := fmt.Sprintf(`{"resourceType":"Patient","id":%q,"gender":%q,"birthDate":"1980-01-%02d","name":[{"family":"Synth%02d","given":["Pat"]}]}`, id, gender, (i%27)+1, i+1)
		out = append(out, Pair{
			ID:           "patient-unchanged-" + id,
			ResourceType: "Patient",
			Category:     CategoryUnchanged,
			R4:           json.RawMessage(r4),
			R5:           json.RawMessage(r4),
			Assertions: []Assertion{
				{Name: "id", R4: "Patient.id", R5: "Patient.id", Want: []string{id}},
				{Name: "gender", R4: "Patient.gender", R5: "Patient.gender", Want: []string{gender}},
			},
		})
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("pat-animal-%02d", i+1)
		r4 := fmt.Sprintf(`{"resourceType":"Patient","id":%q,"gender":"female","animal":{"species":{"text":"canine"}}}`, id)
		r5 := fmt.Sprintf(`{"resourceType":"Patient","id":%q,"gender":"female"}`, id)
		out = append(out, Pair{
			ID:              "patient-loss-" + id,
			ResourceType:    "Patient",
			Category:        CategoryUnchanged,
			R4:              json.RawMessage(r4),
			R5:              json.RawMessage(r5),
			Assertions:      []Assertion{{Name: "id", R4: "Patient.id", R5: "Patient.id", Want: []string{id}}},
			InformationLoss: []string{"Patient.animal removed in R5"},
		})
	}
	return out
}

func observationPairs() []Pair {
	var out []Pair
	codes := []string{"718-7", "6690-2", "2951-2", "2823-3", "2345-7"}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("obs-%02d", i+1)
		code := codes[i%len(codes)]
		r4 := fmt.Sprintf(`{"resourceType":"Observation","id":%q,"status":"final","code":{"coding":[{"system":"http://loinc.org","code":%q}]},"subject":{"reference":"Patient/pat-01"},"valueQuantity":{"value":%d,"unit":"1"}}`, id, code, 10+i)
		out = append(out, Pair{
			ID:           "observation-unchanged-" + id,
			ResourceType: "Observation",
			Category:     CategoryUnchanged,
			R4:           json.RawMessage(r4),
			R5:           json.RawMessage(r4),
			Assertions: []Assertion{
				{Name: "status", R4: "Observation.status", R5: "Observation.status", Want: []string{"final"}},
				{Name: "code", R4: "Observation.code.coding.first().code", R5: "Observation.code.coding.first().code", Want: []string{code}},
			},
		})
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("obs-interp-%02d", i+1)
		r4 := fmt.Sprintf(`{"resourceType":"Observation","id":%q,"status":"final","code":{"coding":[{"code":"718-7"}]},"interpretation":{"coding":[{"code":"N"}]}}`, id)
		r5 := fmt.Sprintf(`{"resourceType":"Observation","id":%q,"status":"final","code":{"coding":[{"code":"718-7"}]},"interpretation":[{"coding":[{"code":"N"}]}]}`, id)
		out = append(out, Pair{
			ID:           "observation-card-" + id,
			ResourceType: "Observation",
			Category:     CategoryCardinality,
			R4:           json.RawMessage(r4),
			R5:           json.RawMessage(r5),
			Assertions: []Assertion{
				{Name: "interp", R4: "Observation.interpretation.coding.first().code", R5: "Observation.interpretation.first().coding.first().code", Want: []string{"N"}},
			},
		})
	}
	return out
}

func conditionPairs() []Pair {
	var out []Pair
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("cond-%02d", i+1)
		prac := fmt.Sprintf("Practitioner/prac-%02d", (i%4)+1)
		r4 := fmt.Sprintf(`{"resourceType":"Condition","id":%q,"clinicalStatus":{"coding":[{"code":"active"}]},"code":{"coding":[{"code":"38341003"}]},"subject":{"reference":"Patient/pat-01"},"asserter":{"reference":%q}}`, id, prac)
		r5 := fmt.Sprintf(`{"resourceType":"Condition","id":%q,"clinicalStatus":{"coding":[{"code":"active"}]},"code":{"coding":[{"code":"38341003"}]},"subject":{"reference":"Patient/pat-01"},"participant":[{"function":{"coding":[{"code":"informant"}]},"actor":{"reference":%q}}]}`, id, prac)
		out = append(out, Pair{
			ID:           "condition-renamed-" + id,
			ResourceType: "Condition",
			Category:     CategoryRenamed,
			R4:           json.RawMessage(r4),
			R5:           json.RawMessage(r5),
			Assertions: []Assertion{
				{Name: "asserter", R4: "Condition.asserter.reference", R5: "Condition.participant.first().actor.reference", Want: []string{prac}},
				{Name: "status", R4: "Condition.clinicalStatus.coding.first().code", R5: "Condition.clinicalStatus.coding.first().code", Want: []string{"active"}},
			},
		})
	}
	return out
}

func medicationRequestPairs() []Pair {
	var out []Pair
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("medreq-%02d", i+1)
		cond := fmt.Sprintf("Condition/cond-%02d", (i%4)+1)
		r4 := fmt.Sprintf(`{"resourceType":"MedicationRequest","id":%q,"status":"active","intent":"order","medicationCodeableConcept":{"coding":[{"code":"313820"}]},"subject":{"reference":"Patient/pat-01"},"reasonCode":[{"coding":[{"code":"38341003"}]}],"reasonReference":[{"reference":%q}]}`, id, cond)
		r5 := fmt.Sprintf(`{"resourceType":"MedicationRequest","id":%q,"status":"active","intent":"order","medication":[{"concept":{"coding":[{"code":"313820"}]}}],"subject":{"reference":"Patient/pat-01"},"reason":[{"concept":{"coding":[{"code":"38341003"}]}},{"reference":{"reference":%q}}]}`, id, cond)
		out = append(out, Pair{
			ID:           "medreq-cc-" + id,
			ResourceType: "MedicationRequest",
			Category:     CategoryCodeableConcept,
			R4:           json.RawMessage(r4),
			R5:           json.RawMessage(r5),
			Assertions: []Assertion{
				{Name: "status", R4: "MedicationRequest.status", R5: "MedicationRequest.status", Want: []string{"active"}},
			},
		})
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("medreq-reported-%02d", i+1)
		r4 := fmt.Sprintf(`{"resourceType":"MedicationRequest","id":%q,"status":"active","intent":"order","medicationCodeableConcept":{"text":"aspirin"},"subject":{"reference":"Patient/pat-01"},"reportedReference":{"reference":"RelatedPerson/rp-1"}}`, id)
		r5 := fmt.Sprintf(`{"resourceType":"MedicationRequest","id":%q,"status":"active","intent":"order","medication":[{"concept":{"text":"aspirin"}}],"subject":{"reference":"Patient/pat-01"},"reported":true,"informationSource":[{"reference":"RelatedPerson/rp-1"}]}`, id)
		out = append(out, Pair{
			ID:              "medreq-type-" + id,
			ResourceType:    "MedicationRequest",
			Category:        CategoryTypeChange,
			R4:              json.RawMessage(r4),
			R5:              json.RawMessage(r5),
			Assertions:      []Assertion{{Name: "status", R4: "MedicationRequest.status", R5: "MedicationRequest.status", Want: []string{"active"}}},
			InformationLoss: []string{"reportedReference collapsed to reported boolean plus informationSource"},
		})
	}
	return out
}
