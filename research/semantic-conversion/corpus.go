package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/degoke/health-ai-stack/research/internal/researchutil"
)

// Pair is one R4 input with expected R5 output and scoring metadata.
type Pair struct {
	ID              string          `json:"id"`
	ResourceType    string          `json:"resourceType"`
	Category        string          `json:"category"`
	R4              json.RawMessage `json:"r4"`
	R5              json.RawMessage `json:"r5"`
	StablePaths     []string        `json:"stablePaths,omitempty"`
	Assertions      []Assertion     `json:"assertions,omitempty"`
	InformationLoss []string        `json:"informationLoss,omitempty"`
	Notes           string          `json:"notes,omitempty"`
}

// Assertion is a FHIRPath boolean check against one version of the pair.
type Assertion struct {
	Version string `json:"version"` // r4 | r5
	Expr    string `json:"expr"`
	Want    bool   `json:"want"`
}

// Corpus returns ≥50 paired R4/R5 instances across four resource types.
func Corpus() []Pair {
	var out []Pair
	out = append(out, patientPairs()...)
	out = append(out, observationPairs()...)
	out = append(out, conditionPairs()...)
	out = append(out, medicationRequestPairs()...)
	return out
}

func patientPairs() []Pair {
	pairs := make([]Pair, 0, 16)
	genders := []string{"female", "male", "other", "unknown"}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("pat-identity-%02d", i)
		gender := genders[i%len(genders)]
		r4 := patientJSON(id, gender, "Rivera"+strconv.Itoa(i), false, "")
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "Patient",
			Category:     "identity",
			R4:           r4,
			R5:           r4,
			StablePaths:  []string{"id", "gender", "name"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "Patient.gender.exists()", Want: true},
				{Version: "r5", Expr: "Patient.id.exists()", Want: true},
			},
			Notes: "Patient core demographics are stable R4→R5.",
		})
	}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("pat-card-%02d", i)
		obj := map[string]any{
			"resourceType": "Patient",
			"id":           id,
			"gender":       "female",
			"identifier": []any{
				map[string]any{"system": "https://example.org/mrn", "value": "MRN-" + strconv.Itoa(i)},
				map[string]any{"system": "https://example.org/ssn-token", "value": "tok-" + strconv.Itoa(i)},
			},
		}
		raw := researchutil.MustJSON(obj)
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "Patient",
			Category:     "cardinality",
			R4:           raw,
			R5:           raw,
			StablePaths:  []string{"id", "identifier"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "Patient.identifier.count() = 2", Want: true},
				{Version: "r5", Expr: "Patient.identifier.count() = 2", Want: true},
			},
			Notes: "identifier 0..* is unchanged; pair documents multi-identifier cardinality.",
		})
	}
	deceasedR4 := researchutil.MustJSON(map[string]any{
		"resourceType":    "Patient",
		"id":              "pat-type-00",
		"gender":          "male",
		"deceasedBoolean": true,
	})
	deceasedR5 := researchutil.MustJSON(map[string]any{
		"resourceType":     "Patient",
		"id":               "pat-type-00",
		"gender":           "male",
		"deceasedDateTime": "2020-01-15T00:00:00Z",
	})
	pairs = append(pairs, Pair{
		ID:           "pat-type-00",
		ResourceType: "Patient",
		Category:     "type",
		R4:           deceasedR4,
		R5:           deceasedR5,
		StablePaths:  []string{"id", "gender"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Patient.deceased.ofType(boolean) = true", Want: true},
			{Version: "r5", Expr: "Patient.deceased.ofType(dateTime).exists()", Want: true},
		},
		Notes: "deceased[x] choice: boolean vs dateTime is a type-shift within the same element.",
	})
	marital := researchutil.MustJSON(map[string]any{
		"resourceType": "Patient",
		"id":           "pat-cc-00",
		"gender":       "female",
		"maritalStatus": map[string]any{
			"coding": []any{map[string]any{
				"system": "http://terminology.hl7.org/CodeSystem/v3-MaritalStatus",
				"code":   "M",
			}},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "pat-cc-00",
		ResourceType: "Patient",
		Category:     "codeableconcept",
		R4:           marital,
		R5:           marital,
		StablePaths:  []string{"id", "maritalStatus"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Patient.maritalStatus.coding.code = 'M'", Want: true},
			{Version: "r5", Expr: "Patient.maritalStatus.coding.code = 'M'", Want: true},
		},
	})
	lost := researchutil.MustJSON(map[string]any{
		"resourceType": "Patient",
		"id":           "pat-loss-00",
		"gender":       "other",
		"extension": []any{map[string]any{
			"url":         "https://example.org/StructureDefinition/legacy-chart-number",
			"valueString": "CHART-99",
		}},
	})
	r5lost := researchutil.MustJSON(map[string]any{
		"resourceType": "Patient",
		"id":           "pat-loss-00",
		"gender":       "other",
	})
	pairs = append(pairs, Pair{
		ID:           "pat-loss-00",
		ResourceType: "Patient",
		Category:     "information_loss",
		R4:           lost,
		R5:           r5lost,
		StablePaths:  []string{"id", "gender"},
		InformationLoss: []string{
			"Patient.extension[https://example.org/StructureDefinition/legacy-chart-number]",
		},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Patient.extension.exists()", Want: true},
			{Version: "r5", Expr: "Patient.extension.exists()", Want: false},
		},
		Notes: "Illustrative local extension dropped during conversion.",
	})
	renamed := researchutil.MustJSON(map[string]any{
		"resourceType": "Patient",
		"id":           "pat-renamed-00",
		"gender":       "female",
		"telecom":      []any{map[string]any{"system": "phone", "value": "+1-555-0100"}},
	})
	pairs = append(pairs, Pair{
		ID:           "pat-renamed-00",
		ResourceType: "Patient",
		Category:     "renamed",
		R4:           renamed,
		R5:           renamed,
		StablePaths:  []string{"id", "telecom"},
		Notes:        "telecom is stable; included so each category has Patient coverage.",
		Assertions: []Assertion{
			{Version: "r4", Expr: "Patient.telecom.value.exists()", Want: true},
			{Version: "r5", Expr: "Patient.telecom.value.exists()", Want: true},
		},
	})
	return pairs
}

func observationPairs() []Pair {
	var pairs []Pair
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("obs-identity-%02d", i)
		r4 := observationJSON(id, "final", "8867-4", 60+float64(i), nil, "")
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "Observation",
			Category:     "identity",
			R4:           r4,
			R5:           r4,
			StablePaths:  []string{"id", "status", "code", "valueQuantity"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "Observation.status = 'final'", Want: true},
				{Version: "r5", Expr: "Observation.value.ofType(Quantity).value.exists()", Want: true},
			},
		})
	}
	specR4 := observationJSON("obs-card-00", "final", "8867-4", 70, map[string]any{
		"specimen": map[string]any{"reference": "Specimen/sp-1"},
	}, "")
	specR5 := observationJSON("obs-card-00", "final", "8867-4", 70, map[string]any{
		"specimen": []any{map[string]any{"reference": "Specimen/sp-1"}},
	}, "")
	pairs = append(pairs, Pair{
		ID:           "obs-card-00",
		ResourceType: "Observation",
		Category:     "cardinality",
		R4:           specR4,
		R5:           specR5,
		StablePaths:  []string{"id", "status", "code"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Observation.specimen.exists()", Want: true},
			{Version: "r5", Expr: "Observation.specimen.exists()", Want: true},
		},
		Notes: "Observation.specimen cardinality 0..1 (R4) → 0..* (R5).",
	})
	pairs = append(pairs, Pair{
		ID:           "obs-card-01",
		ResourceType: "Observation",
		Category:     "cardinality",
		R4:           observationJSON("obs-card-01", "final", "8867-4", 70, map[string]any{"specimen": map[string]any{"reference": "Specimen/sp-1"}}, ""),
		R5: observationJSON("obs-card-01", "final", "8867-4", 70, map[string]any{
			"specimen": []any{
				map[string]any{"reference": "Specimen/sp-1"},
				map[string]any{"reference": "Specimen/sp-2"},
			},
		}, ""),
		StablePaths: []string{"id", "status"},
		Notes:       "R5 may carry additional specimen references.",
		Assertions: []Assertion{
			{Version: "r5", Expr: "Observation.specimen.count() = 2", Want: true},
		},
	})
	bodyR4 := observationJSON("obs-type-00", "final", "8310-5", 37.2, map[string]any{
		"bodySite": map[string]any{
			"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "368209003"}},
		},
	}, "")
	bodyR5 := observationJSON("obs-type-00", "final", "8310-5", 37.2, map[string]any{
		"bodySite": []any{map[string]any{
			"site": map[string]any{
				"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "368209003"}},
			},
		}},
	}, "")
	pairs = append(pairs, Pair{
		ID:           "obs-type-00",
		ResourceType: "Observation",
		Category:     "type",
		R4:           bodyR4,
		R5:           bodyR5,
		StablePaths:  []string{"id", "status", "code"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Observation.bodySite.coding.code.exists()", Want: true},
			{Version: "r5", Expr: "Observation.bodySite.site.coding.code.exists()", Want: true},
		},
		Notes: "Observation.bodySite CodeableConcept (R4) → BackboneElement.site (R5).",
	})
	cc := observationJSON("obs-cc-00", "final", "8480-6", 120, map[string]any{
		"category": []any{map[string]any{
			"coding": []any{map[string]any{
				"system": "http://terminology.hl7.org/CodeSystem/observation-category",
				"code":   "vital-signs",
			}},
		}},
	}, "")
	pairs = append(pairs, Pair{
		ID:           "obs-cc-00",
		ResourceType: "Observation",
		Category:     "codeableconcept",
		R4:           cc,
		R5:           cc,
		StablePaths:  []string{"id", "category"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Observation.category.coding.code = 'vital-signs'", Want: true},
			{Version: "r5", Expr: "Observation.category.coding.code = 'vital-signs'", Want: true},
		},
	})
	interpR4 := observationJSON("obs-renamed-00", "final", "8867-4", 80, map[string]any{
		"performer": []any{map[string]any{"reference": "Practitioner/pr-1"}},
	}, "")
	pairs = append(pairs, Pair{
		ID:           "obs-renamed-00",
		ResourceType: "Observation",
		Category:     "renamed",
		R4:           interpR4,
		R5:           interpR4,
		StablePaths:  []string{"id", "performer"},
		Notes:        "performer is stable R4→R5; pair documents a named actor reference.",
		Assertions: []Assertion{
			{Version: "r4", Expr: "Observation.performer.exists()", Want: true},
		},
	})
	commentR4 := observationJSON("obs-loss-00", "final", "8867-4", 55, map[string]any{
		"note": []any{map[string]any{"text": "legacy device annotation"}},
	}, "")
	commentR5 := observationJSON("obs-loss-00", "final", "8867-4", 55, nil, "")
	pairs = append(pairs, Pair{
		ID:              "obs-loss-00",
		ResourceType:    "Observation",
		Category:        "information_loss",
		R4:              commentR4,
		R5:              commentR5,
		StablePaths:     []string{"id", "status"},
		InformationLoss: []string{"Observation.note"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Observation.note.exists()", Want: true},
			{Version: "r5", Expr: "Observation.note.exists()", Want: false},
		},
		Notes: "Illustrative drop of narrative note when a converter cannot preserve it.",
	})
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("obs-identity-x-%02d", i)
		r4 := observationJSON(id, "final", "2708-6", 96+float64(i), nil, "")
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "Observation",
			Category:     "identity",
			R4:           r4,
			R5:           r4,
			StablePaths:  []string{"id", "code"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "Observation.code.coding.code = '2708-6'", Want: true},
			},
		})
	}
	return pairs
}

func conditionPairs() []Pair {
	var pairs []Pair
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("cond-identity-%02d", i)
		r4 := conditionJSON(id, "active", "38341003", "", nil)
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "Condition",
			Category:     "identity",
			R4:           r4,
			R5:           r4,
			StablePaths:  []string{"id", "clinicalStatus", "code", "subject"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "Condition.clinicalStatus.coding.code = 'active'", Want: true},
				{Version: "r5", Expr: "Condition.subject.exists()", Want: true},
			},
		})
	}
	asserterR4 := conditionJSON("cond-renamed-00", "active", "38341003", "Practitioner/pr-1", nil)
	asserterR5 := conditionJSON("cond-renamed-00", "active", "38341003", "", map[string]any{
		"participant": []any{map[string]any{
			"function": map[string]any{
				"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/v3-ParticipationType",
					"code":   "AUTHEN",
				}},
			},
			"actor": map[string]any{"reference": "Practitioner/pr-1"},
		}},
	})
	pairs = append(pairs, Pair{
		ID:           "cond-renamed-00",
		ResourceType: "Condition",
		Category:     "renamed",
		R4:           asserterR4,
		R5:           asserterR5,
		StablePaths:  []string{"id", "code", "subject"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Condition.asserter.exists()", Want: true},
			{Version: "r5", Expr: "Condition.participant.actor.exists()", Want: true},
		},
		Notes: "Condition.asserter (R4) → Condition.participant (R5).",
	})
	pairs = append(pairs, Pair{
		ID:           "cond-renamed-01",
		ResourceType: "Condition",
		Category:     "renamed",
		R4:           conditionJSON("cond-renamed-01", "active", "44054006", "Practitioner/pr-2", nil),
		R5: conditionJSON("cond-renamed-01", "active", "44054006", "", map[string]any{
			"participant": []any{map[string]any{
				"actor": map[string]any{"reference": "Practitioner/pr-2"},
			}},
		}),
		StablePaths: []string{"id", "subject"},
		Notes:       "Diabetes mellitus asserter → participant.",
		Assertions: []Assertion{
			{Version: "r5", Expr: "Condition.participant.exists()", Want: true},
		},
	})
	cat := conditionJSON("cond-card-00", "active", "38341003", "", map[string]any{
		"category": []any{
			map[string]any{"coding": []any{map[string]any{"system": "http://terminology.hl7.org/CodeSystem/condition-category", "code": "problem-list-item"}}},
			map[string]any{"coding": []any{map[string]any{"system": "http://terminology.hl7.org/CodeSystem/condition-category", "code": "encounter-diagnosis"}}},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "cond-card-00",
		ResourceType: "Condition",
		Category:     "cardinality",
		R4:           cat,
		R5:           cat,
		StablePaths:  []string{"id", "category"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Condition.category.count() = 2", Want: true},
			{Version: "r5", Expr: "Condition.category.count() = 2", Want: true},
		},
	})
	sev := conditionJSON("cond-cc-00", "active", "38341003", "", map[string]any{
		"severity": map[string]any{
			"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "24484000"}},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "cond-cc-00",
		ResourceType: "Condition",
		Category:     "codeableconcept",
		R4:           sev,
		R5:           sev,
		StablePaths:  []string{"id", "severity"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Condition.severity.coding.code.exists()", Want: true},
		},
	})
	onsetR4 := conditionJSON("cond-type-00", "active", "38341003", "", map[string]any{
		"onsetDateTime": "2018-03-01",
	})
	onsetR5 := conditionJSON("cond-type-00", "active", "38341003", "", map[string]any{
		"onsetAge": map[string]any{"value": 54, "unit": "a", "system": "http://unitsofmeasure.org", "code": "a"},
	})
	pairs = append(pairs, Pair{
		ID:           "cond-type-00",
		ResourceType: "Condition",
		Category:     "type",
		R4:           onsetR4,
		R5:           onsetR5,
		StablePaths:  []string{"id", "code"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "Condition.onset.ofType(dateTime).exists()", Want: true},
			{Version: "r5", Expr: "Condition.onset.ofType(Age).exists()", Want: true},
		},
		Notes: "onset[x] choice type shift dateTime → Age.",
	})
	evR4 := conditionJSON("cond-loss-00", "active", "38341003", "", map[string]any{
		"evidence": []any{map[string]any{
			"detail": []any{map[string]any{"reference": "Observation/obs-sbp-1"}},
		}},
	})
	evR5 := conditionJSON("cond-loss-00", "active", "38341003", "", nil)
	pairs = append(pairs, Pair{
		ID:              "cond-loss-00",
		ResourceType:    "Condition",
		Category:        "information_loss",
		R4:              evR4,
		R5:              evR5,
		StablePaths:     []string{"id", "code"},
		InformationLoss: []string{"Condition.evidence"},
		Notes:           "Condition.evidence was removed in R5.",
		Assertions: []Assertion{
			{Version: "r4", Expr: "Condition.evidence.exists()", Want: true},
			{Version: "r5", Expr: "Condition.evidence.exists()", Want: false},
		},
	})
	pairs = append(pairs, Pair{
		ID:              "cond-loss-01",
		ResourceType:    "Condition",
		Category:        "information_loss",
		R4:              conditionJSON("cond-loss-01", "inactive", "38341003", "", map[string]any{"evidence": []any{map[string]any{"code": []any{map[string]any{"text": "chart review"}}}}}),
		R5:              conditionJSON("cond-loss-01", "inactive", "38341003", "", nil),
		StablePaths:     []string{"id"},
		InformationLoss: []string{"Condition.evidence"},
		Assertions: []Assertion{
			{Version: "r5", Expr: "Condition.clinicalStatus.coding.code = 'inactive'", Want: true},
		},
	})
	return pairs
}

func medicationRequestPairs() []Pair {
	var pairs []Pair
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("medreq-identity-%02d", i)
		r4 := medReqJSON(id, "active", "314076", nil)
		pairs = append(pairs, Pair{
			ID:           id,
			ResourceType: "MedicationRequest",
			Category:     "identity",
			R4:           r4,
			R5:           r4,
			StablePaths:  []string{"id", "status", "intent", "subject"},
			Assertions: []Assertion{
				{Version: "r4", Expr: "MedicationRequest.status = 'active'", Want: true},
				{Version: "r5", Expr: "MedicationRequest.intent = 'order'", Want: true},
			},
		})
	}
	reasonR4 := medReqJSON("medreq-renamed-00", "active", "314076", map[string]any{
		"reasonCode": []any{map[string]any{
			"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "38341003"}},
		}},
	})
	reasonR5 := medReqJSON("medreq-renamed-00", "active", "314076", map[string]any{
		"reason": []any{map[string]any{
			"concept": map[string]any{
				"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "38341003"}},
			},
		}},
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-renamed-00",
		ResourceType: "MedicationRequest",
		Category:     "renamed",
		R4:           reasonR4,
		R5:           reasonR5,
		StablePaths:  []string{"id", "status"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "MedicationRequest.reasonCode.exists()", Want: true},
			{Version: "r5", Expr: "MedicationRequest.reason.exists()", Want: true},
		},
		Notes: "reasonCode/reasonReference (R4) → reason CodeableReference (R5).",
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-renamed-01",
		ResourceType: "MedicationRequest",
		Category:     "renamed",
		R4: medReqJSON("medreq-renamed-01", "active", "314076", map[string]any{
			"reasonReference": []any{map[string]any{"reference": "Condition/cond-identity-00"}},
		}),
		R5: medReqJSON("medreq-renamed-01", "active", "314076", map[string]any{
			"reason": []any{map[string]any{
				"reference": map[string]any{"reference": "Condition/cond-identity-00"},
			}},
		}),
		StablePaths: []string{"id"},
		Assertions: []Assertion{
			{Version: "r5", Expr: "MedicationRequest.reason.reference.exists()", Want: true},
		},
	})
	medR5 := researchutil.MustJSON(map[string]any{
		"resourceType": "MedicationRequest",
		"id":           "medreq-type-00",
		"status":       "active",
		"intent":       "order",
		"subject":      map[string]any{"reference": "Patient/pat-1"},
		"medication": map[string]any{
			"concept": map[string]any{
				"coding": []any{map[string]any{"system": "http://www.nlm.nih.gov/research/umls/rxnorm", "code": "314076"}},
				"text":   "Lisinopril",
			},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-type-00",
		ResourceType: "MedicationRequest",
		Category:     "type",
		R4:           medReqJSON("medreq-type-00", "active", "314076", nil),
		R5:           medR5,
		StablePaths:  []string{"id", "status", "intent"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "MedicationRequest.medication.ofType(CodeableConcept).exists()", Want: true},
			{Version: "r5", Expr: "MedicationRequest.medication.concept.exists()", Want: true},
		},
		Notes: "medication[x] (R4) → medication CodeableReference (R5).",
	})
	dose := medReqJSON("medreq-card-00", "active", "314076", map[string]any{
		"dosageInstruction": []any{
			map[string]any{"text": "5 mg oral daily"},
			map[string]any{"text": "hold if hypotensive"},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-card-00",
		ResourceType: "MedicationRequest",
		Category:     "cardinality",
		R4:           dose,
		R5:           dose,
		StablePaths:  []string{"id", "dosageInstruction"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "MedicationRequest.dosageInstruction.count() = 2", Want: true},
		},
	})
	cc := medReqJSON("medreq-cc-00", "active", "314076", map[string]any{
		"category": []any{map[string]any{
			"coding": []any{map[string]any{"system": "http://terminology.hl7.org/CodeSystem/medicationrequest-category", "code": "community"}},
		}},
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-cc-00",
		ResourceType: "MedicationRequest",
		Category:     "codeableconcept",
		R4:           cc,
		R5:           cc,
		StablePaths:  []string{"id", "category"},
		Assertions: []Assertion{
			{Version: "r4", Expr: "MedicationRequest.category.coding.code = 'community'", Want: true},
		},
	})
	detR4 := medReqJSON("medreq-loss-00", "active", "314076", map[string]any{
		"detectedIssue": []any{map[string]any{"reference": "DetectedIssue/di-1"}},
	})
	detR5 := medReqJSON("medreq-loss-00", "active", "314076", nil)
	pairs = append(pairs, Pair{
		ID:              "medreq-loss-00",
		ResourceType:    "MedicationRequest",
		Category:        "information_loss",
		R4:              detR4,
		R5:              detR5,
		StablePaths:     []string{"id", "status"},
		InformationLoss: []string{"MedicationRequest.detectedIssue"},
		Notes:           "detectedIssue is not present on R5 MedicationRequest.",
		Assertions: []Assertion{
			{Version: "r4", Expr: "MedicationRequest.detectedIssue.exists()", Want: true},
			{Version: "r5", Expr: "MedicationRequest.detectedIssue.exists()", Want: false},
		},
	})
	pairs = append(pairs, Pair{
		ID:           "medreq-loss-01",
		ResourceType: "MedicationRequest",
		Category:     "information_loss",
		R4: medReqJSON("medreq-loss-01", "cancelled", "314076", map[string]any{
			"detectedIssue": []any{map[string]any{"reference": "DetectedIssue/di-2"}},
		}),
		R5:              medReqJSON("medreq-loss-01", "cancelled", "314076", nil),
		StablePaths:     []string{"id"},
		InformationLoss: []string{"MedicationRequest.detectedIssue"},
		Assertions: []Assertion{
			{Version: "r5", Expr: "MedicationRequest.status = 'cancelled'", Want: true},
		},
	})
	return pairs
}

func patientJSON(id, gender, family string, deceased bool, extraNote string) json.RawMessage {
	obj := map[string]any{
		"resourceType": "Patient",
		"id":           id,
		"gender":       gender,
		"name":         []any{map[string]any{"family": family, "given": []any{"Ana"}}},
	}
	if deceased {
		obj["deceasedBoolean"] = true
	}
	_ = extraNote
	return researchutil.MustJSON(obj)
}

func observationJSON(id, status, code string, value float64, extra map[string]any, _ string) json.RawMessage {
	obj := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       status,
		"code": map[string]any{
			"coding": []any{map[string]any{"system": "http://loinc.org", "code": code}},
			"text":   code,
		},
		"subject":       map[string]any{"reference": "Patient/pat-1"},
		"valueQuantity": map[string]any{"value": value, "unit": "1", "system": "http://unitsofmeasure.org"},
	}
	for k, v := range extra {
		obj[k] = v
	}
	return researchutil.MustJSON(obj)
}

func conditionJSON(id, clinical, code, asserter string, extra map[string]any) json.RawMessage {
	obj := map[string]any{
		"resourceType": "Condition",
		"id":           id,
		"clinicalStatus": map[string]any{
			"coding": []any{map[string]any{
				"system": "http://terminology.hl7.org/CodeSystem/condition-clinical",
				"code":   clinical,
			}},
		},
		"subject": map[string]any{"reference": "Patient/pat-1"},
		"code": map[string]any{
			"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": code}},
		},
	}
	if asserter != "" {
		obj["asserter"] = map[string]any{"reference": asserter}
	}
	for k, v := range extra {
		obj[k] = v
	}
	return researchutil.MustJSON(obj)
}

func medReqJSON(id, status, rxnorm string, extra map[string]any) json.RawMessage {
	obj := map[string]any{
		"resourceType": "MedicationRequest",
		"id":           id,
		"status":       status,
		"intent":       "order",
		"subject":      map[string]any{"reference": "Patient/pat-1"},
		"medicationCodeableConcept": map[string]any{
			"coding": []any{map[string]any{"system": "http://www.nlm.nih.gov/research/umls/rxnorm", "code": rxnorm}},
			"text":   "Lisinopril",
		},
	}
	for k, v := range extra {
		obj[k] = v
	}
	return researchutil.MustJSON(obj)
}
