package main

import (
	"encoding/json"

	"github.com/degoke/health-ai-stack/research/internal/researchutil"
)

// Pair is one R4 input with expected R5 output and scoring metadata.
type Pair struct {
	ID              string           `json:"id"`
	ResourceType    string           `json:"resourceType"`
	Category        string           `json:"category"`
	R4              json.RawMessage  `json:"r4"`
	R5              json.RawMessage  `json:"r5"`
	StablePaths     []string         `json:"stablePaths,omitempty"`
	R4FHIRPath      []FHIRPathAssert `json:"r4Fhirpath,omitempty"`
	R5JSON          []JSONCheck      `json:"r5Json,omitempty"`
	InformationLoss []string         `json:"informationLoss,omitempty"`
	Notes           string           `json:"notes,omitempty"`
}

// FHIRPathAssert is a pkg/fhirpath boolean check against the R4 proto envelope.
type FHIRPathAssert struct {
	Expr string `json:"expr"`
	Want bool   `json:"want"`
}

// JSONCheck is a structural check against R5 JSON (not FHIRPath).
type JSONCheck struct {
	Path  string `json:"path"`
	Op    string `json:"op"` // exists | missing | count | equals
	Value any    `json:"value,omitempty"`
}

func fp(expr string, want bool) FHIRPathAssert {
	return FHIRPathAssert{Expr: expr, Want: want}
}

func exists(path string) JSONCheck  { return JSONCheck{Path: path, Op: "exists"} }
func missing(path string) JSONCheck { return JSONCheck{Path: path, Op: "missing"} }
func count(path string, n int) JSONCheck {
	return JSONCheck{Path: path, Op: "count", Value: n}
}
func equals(path string, v any) JSONCheck {
	return JSONCheck{Path: path, Op: "equals", Value: v}
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
	return []Pair{
		{
			ID: "pat-identity-demographics", ResourceType: "Patient", Category: "identity",
			R4: patientObj("pat-identity-demographics", map[string]any{
				"gender":    "female",
				"birthDate": "1984-02-11",
				"name":      []any{map[string]any{"family": "Rivera", "given": []any{"Ana"}}},
			}),
			R5: patientObj("pat-identity-demographics", map[string]any{
				"gender":    "female",
				"birthDate": "1984-02-11",
				"name":      []any{map[string]any{"family": "Rivera", "given": []any{"Ana"}}},
			}),
			StablePaths: []string{"id", "gender", "name", "birthDate"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.gender = 'female'", true), fp("Patient.birthDate.exists()", true)},
			R5JSON:      []JSONCheck{equals("gender", "female"), exists("birthDate")},
			Notes:       "Core demographics are stable R4→R5.",
		},
		{
			ID: "pat-identity-address", ResourceType: "Patient", Category: "identity",
			R4: patientObj("pat-identity-address", map[string]any{
				"gender": "male",
				"address": []any{map[string]any{
					"use": "home", "city": "Portland", "state": "OR", "postalCode": "97201",
				}},
			}),
			R5: patientObj("pat-identity-address", map[string]any{
				"gender": "male",
				"address": []any{map[string]any{
					"use": "home", "city": "Portland", "state": "OR", "postalCode": "97201",
				}},
			}),
			StablePaths: []string{"id", "address"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.address.exists()", true)},
			R5JSON:      []JSONCheck{equals("address.city", "Portland")},
		},
		{
			ID: "pat-identity-contact", ResourceType: "Patient", Category: "identity",
			R4: patientObj("pat-identity-contact", map[string]any{
				"gender": "female",
				"contact": []any{map[string]any{
					"relationship": []any{map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/v2-0131", "code": "N",
					}}}},
					"name": map[string]any{"family": "Cho"},
				}},
			}),
			R5: patientObj("pat-identity-contact", map[string]any{
				"gender": "female",
				"contact": []any{map[string]any{
					"relationship": []any{map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/v2-0131", "code": "N",
					}}}},
					"name": map[string]any{"family": "Cho"},
				}},
			}),
			StablePaths: []string{"id", "contact"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.contact.exists()", true)},
			R5JSON:      []JSONCheck{exists("contact.name.family")},
		},
		{
			ID: "pat-identity-communication", ResourceType: "Patient", Category: "identity",
			R4: patientObj("pat-identity-communication", map[string]any{
				"gender": "other",
				"communication": []any{map[string]any{
					"language": map[string]any{"coding": []any{map[string]any{
						"system": "urn:ietf:bcp:47", "code": "es",
					}}},
					"preferred": true,
				}},
			}),
			R5: patientObj("pat-identity-communication", map[string]any{
				"gender": "other",
				"communication": []any{map[string]any{
					"language": map[string]any{"coding": []any{map[string]any{
						"system": "urn:ietf:bcp:47", "code": "es",
					}}},
					"preferred": true,
				}},
			}),
			StablePaths: []string{"id", "communication"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.communication.exists()", true)},
			R5JSON:      []JSONCheck{equals("communication.language.coding.code", "es")},
		},
		{
			ID: "pat-card-identifier-additional", ResourceType: "Patient", Category: "cardinality",
			R4: patientObj("pat-card-identifier-additional", map[string]any{
				"gender": "female",
				"identifier": []any{
					map[string]any{"system": "https://example.org/mrn", "value": "MRN-1"},
				},
			}),
			R5: patientObj("pat-card-identifier-additional", map[string]any{
				"gender": "female",
				"identifier": []any{
					map[string]any{"system": "https://example.org/mrn", "value": "MRN-1"},
					map[string]any{"system": "https://example.org/ssn-token", "value": "tok-1"},
				},
			}),
			StablePaths: []string{"id", "gender"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.identifier.count() = 1", true)},
			R5JSON:      []JSONCheck{count("identifier", 2)},
			Notes:       "identifier remains 0..*; R5 instance carries an extra repetition.",
		},
		{
			ID: "pat-type-deceased-boolean-to-datetime", ResourceType: "Patient", Category: "type",
			R4: patientObj("pat-type-deceased-boolean-to-datetime", map[string]any{
				"gender":          "male",
				"deceasedBoolean": true,
			}),
			R5: patientObj("pat-type-deceased-boolean-to-datetime", map[string]any{
				"gender":           "male",
				"deceasedDateTime": "2020-01-15T00:00:00Z",
			}),
			StablePaths: []string{"id", "gender"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.deceased.ofType(boolean).exists()", true)},
			R5JSON:      []JSONCheck{exists("deceasedDateTime"), missing("deceasedBoolean")},
			Notes:       "deceased[x] choice: boolean vs dateTime.",
		},
		{
			ID: "pat-type-multiple-birth-boolean-to-integer", ResourceType: "Patient", Category: "type",
			R4: patientObj("pat-type-multiple-birth-boolean-to-integer", map[string]any{
				"gender":               "female",
				"multipleBirthBoolean": true,
			}),
			R5: patientObj("pat-type-multiple-birth-boolean-to-integer", map[string]any{
				"gender":               "female",
				"multipleBirthInteger": 2,
			}),
			StablePaths: []string{"id", "gender"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.multipleBirth.ofType(boolean).exists()", true)},
			R5JSON:      []JSONCheck{equals("multipleBirthInteger", 2), missing("multipleBirthBoolean")},
			Notes:       "multipleBirth[x] choice: boolean vs integer.",
		},
		{
			ID: "pat-cc-marital-display", ResourceType: "Patient", Category: "codeableconcept",
			R4: patientObj("pat-cc-marital-display", map[string]any{
				"gender": "female",
				"maritalStatus": map[string]any{
					"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/v3-MaritalStatus",
						"code":   "M",
					}},
				},
			}),
			R5: patientObj("pat-cc-marital-display", map[string]any{
				"gender": "female",
				"maritalStatus": map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://terminology.hl7.org/CodeSystem/v3-MaritalStatus",
						"code":    "M",
						"display": "Married",
					}},
					"text": "Married",
				},
			}),
			StablePaths: []string{"id", "gender"},
			R4FHIRPath:  []FHIRPathAssert{fp("Patient.maritalStatus.coding.code = 'M'", true)},
			R5JSON:      []JSONCheck{equals("maritalStatus.coding.code", "M"), equals("maritalStatus.coding.display", "Married")},
		},
		{
			ID: "pat-loss-legacy-extension", ResourceType: "Patient", Category: "information_loss",
			R4: patientObj("pat-loss-legacy-extension", map[string]any{
				"gender": "other",
				"extension": []any{map[string]any{
					"url":         "https://example.org/StructureDefinition/legacy-chart-number",
					"valueString": "CHART-99",
				}},
			}),
			R5:              patientObj("pat-loss-legacy-extension", map[string]any{"gender": "other"}),
			StablePaths:     []string{"id", "gender"},
			InformationLoss: []string{"Patient.extension[https://example.org/StructureDefinition/legacy-chart-number]"},
			R4FHIRPath:      []FHIRPathAssert{fp("Patient.extension.exists()", true)},
			R5JSON:          []JSONCheck{missing("extension")},
			Notes:           "Illustrative local extension dropped during conversion.",
		},
		{
			ID: "pat-loss-photo", ResourceType: "Patient", Category: "information_loss",
			R4: patientObj("pat-loss-photo", map[string]any{
				"gender": "unknown",
				"photo":  []any{map[string]any{"contentType": "image/png", "title": "legacy-id-photo"}},
			}),
			R5:              patientObj("pat-loss-photo", map[string]any{"gender": "unknown"}),
			StablePaths:     []string{"id", "gender"},
			InformationLoss: []string{"Patient.photo"},
			R4FHIRPath:      []FHIRPathAssert{fp("Patient.photo.exists()", true)},
			R5JSON:          []JSONCheck{missing("photo")},
			Notes:           "Illustrative drop of Patient.photo when a converter cannot preserve attachments.",
		},
	}
}

func observationPairs() []Pair {
	return []Pair{
		{
			ID: "obs-identity-heart-rate", ResourceType: "Observation", Category: "identity",
			R4:          observationJSON("obs-identity-heart-rate", "final", "8867-4", 72, nil),
			R5:          observationJSON("obs-identity-heart-rate", "final", "8867-4", 72, nil),
			StablePaths: []string{"id", "status", "code", "valueQuantity"},
			R4FHIRPath: []FHIRPathAssert{
				fp("Observation.status = 'final'", true),
				fp("Observation.value.ofType(Quantity).value.exists()", true),
			},
			R5JSON: []JSONCheck{equals("status", "final"), exists("valueQuantity.value")},
		},
		{
			ID: "obs-identity-systolic-bp", ResourceType: "Observation", Category: "identity",
			R4:          observationJSON("obs-identity-systolic-bp", "final", "8480-6", 128, nil),
			R5:          observationJSON("obs-identity-systolic-bp", "final", "8480-6", 128, nil),
			StablePaths: []string{"id", "status", "code", "valueQuantity"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.code.coding.code = '8480-6'", true)},
			R5JSON:      []JSONCheck{equals("code.coding.code", "8480-6")},
		},
		{
			ID: "obs-identity-spo2", ResourceType: "Observation", Category: "identity",
			R4:          observationJSON("obs-identity-spo2", "amended", "2708-6", 96, nil),
			R5:          observationJSON("obs-identity-spo2", "amended", "2708-6", 96, nil),
			StablePaths: []string{"id", "status", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.status = 'amended'", true)},
			R5JSON:      []JSONCheck{equals("status", "amended")},
		},
		{
			ID: "obs-identity-bp-panel", ResourceType: "Observation", Category: "identity",
			R4: observationJSON("obs-identity-bp-panel", "final", "85354-9", 0, map[string]any{
				"component": []any{
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8480-6"}}},
						"valueQuantity": map[string]any{"value": 120, "unit": "mmHg"},
					},
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8462-4"}}},
						"valueQuantity": map[string]any{"value": 80, "unit": "mmHg"},
					},
				},
			}),
			R5: observationJSON("obs-identity-bp-panel", "final", "85354-9", 0, map[string]any{
				"component": []any{
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8480-6"}}},
						"valueQuantity": map[string]any{"value": 120, "unit": "mmHg"},
					},
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8462-4"}}},
						"valueQuantity": map[string]any{"value": 80, "unit": "mmHg"},
					},
				},
			}),
			StablePaths: []string{"id", "component"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.component.count() = 2", true)},
			R5JSON:      []JSONCheck{count("component", 2)},
		},
		{
			ID: "obs-card-specimen-singleton-to-array", ResourceType: "Observation", Category: "cardinality",
			R4: observationJSON("obs-card-specimen-singleton-to-array", "final", "8867-4", 70, map[string]any{
				"specimen": map[string]any{"reference": "Specimen/sp-1"},
			}),
			R5: observationJSON("obs-card-specimen-singleton-to-array", "final", "8867-4", 70, map[string]any{
				"specimen": []any{map[string]any{"reference": "Specimen/sp-1"}},
			}),
			StablePaths: []string{"id", "status", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.specimen.exists()", true)},
			R5JSON:      []JSONCheck{count("specimen", 1), equals("specimen.reference", "Specimen/sp-1")},
			Notes:       "Observation.specimen cardinality 0..1 (R4) → 0..* (R5).",
		},
		{
			ID: "obs-card-specimen-additional", ResourceType: "Observation", Category: "cardinality",
			R4: observationJSON("obs-card-specimen-additional", "final", "8867-4", 70, map[string]any{
				"specimen": map[string]any{"reference": "Specimen/sp-1"},
			}),
			R5: observationJSON("obs-card-specimen-additional", "final", "8867-4", 70, map[string]any{
				"specimen": []any{
					map[string]any{"reference": "Specimen/sp-1"},
					map[string]any{"reference": "Specimen/sp-2"},
				},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.specimen.exists()", true)},
			R5JSON:      []JSONCheck{count("specimen", 2)},
			Notes:       "R5 may carry additional specimen references.",
		},
		{
			ID: "obs-card-category-additional", ResourceType: "Observation", Category: "cardinality",
			R4: observationJSON("obs-card-category-additional", "final", "8867-4", 70, map[string]any{
				"category": []any{map[string]any{"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "vital-signs",
				}}}},
			}),
			R5: observationJSON("obs-card-category-additional", "final", "8867-4", 70, map[string]any{
				"category": []any{
					map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "vital-signs",
					}}},
					map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "survey",
					}}},
				},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.category.count() = 1", true)},
			R5JSON:      []JSONCheck{count("category", 2)},
		},
		{
			ID: "obs-card-performer-additional", ResourceType: "Observation", Category: "cardinality",
			R4: observationJSON("obs-card-performer-additional", "final", "8867-4", 80, map[string]any{
				"performer": []any{map[string]any{"reference": "Practitioner/pr-1"}},
			}),
			R5: observationJSON("obs-card-performer-additional", "final", "8867-4", 80, map[string]any{
				"performer": []any{
					map[string]any{"reference": "Practitioner/pr-1"},
					map[string]any{"reference": "Organization/org-1"},
				},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.performer.count() = 1", true)},
			R5JSON:      []JSONCheck{count("performer", 2)},
		},
		{
			ID: "obs-card-component-additional", ResourceType: "Observation", Category: "cardinality",
			R4: observationJSON("obs-card-component-additional", "final", "85354-9", 0, map[string]any{
				"component": []any{map[string]any{
					"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8480-6"}}},
					"valueQuantity": map[string]any{"value": 120},
				}},
			}),
			R5: observationJSON("obs-card-component-additional", "final", "85354-9", 0, map[string]any{
				"component": []any{
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8480-6"}}},
						"valueQuantity": map[string]any{"value": 120},
					},
					map[string]any{
						"code":          map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8462-4"}}},
						"valueQuantity": map[string]any{"value": 80},
					},
				},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.component.count() = 1", true)},
			R5JSON:      []JSONCheck{count("component", 2)},
		},
		{
			ID: "obs-type-bodysite-codeableconcept-to-backbone", ResourceType: "Observation", Category: "type",
			R4: observationJSON("obs-type-bodysite-codeableconcept-to-backbone", "final", "8310-5", 37.2, map[string]any{
				"bodySite": map[string]any{
					"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "368209003"}},
				},
			}),
			R5: observationJSON("obs-type-bodysite-codeableconcept-to-backbone", "final", "8310-5", 37.2, map[string]any{
				"bodySite": []any{map[string]any{
					"site": map[string]any{
						"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "368209003"}},
					},
				}},
			}),
			StablePaths: []string{"id", "status", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.bodySite.coding.code.exists()", true)},
			R5JSON:      []JSONCheck{exists("bodySite.site.coding.code"), missing("bodySite.coding")},
			Notes:       "Observation.bodySite CodeableConcept (R4) → BackboneElement.site (R5).",
		},
		{
			ID: "obs-type-value-quantity-to-codeableconcept", ResourceType: "Observation", Category: "type",
			R4: observationJSON("obs-type-value-quantity-to-codeableconcept", "final", "72166-2", 0, nil),
			R5: observationJSON("obs-type-value-quantity-to-codeableconcept", "final", "72166-2", 0, map[string]any{
				"valueCodeableConcept": map[string]any{
					"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "266919005"}},
					"text":   "Never smoked",
				},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.value.ofType(Quantity).exists()", true)},
			R5JSON:      []JSONCheck{exists("valueCodeableConcept"), missing("valueQuantity")},
			Notes:       "value[x] choice shift Quantity → CodeableConcept.",
		},
		{
			ID: "obs-type-effective-datetime-to-period", ResourceType: "Observation", Category: "type",
			R4: observationJSON("obs-type-effective-datetime-to-period", "final", "8867-4", 64, map[string]any{
				"effectiveDateTime": "2026-01-02T08:00:00Z",
			}),
			R5: observationJSON("obs-type-effective-datetime-to-period", "final", "8867-4", 64, map[string]any{
				"effectivePeriod": map[string]any{"start": "2026-01-02T08:00:00Z", "end": "2026-01-02T08:05:00Z"},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.effective.ofType(dateTime).exists()", true)},
			R5JSON:      []JSONCheck{exists("effectivePeriod.start"), missing("effectiveDateTime")},
			Notes:       "effective[x] choice shift dateTime → Period.",
		},
		{
			ID: "obs-type-value-string-to-quantity", ResourceType: "Observation", Category: "type",
			R4: observationJSON("obs-type-value-string-to-quantity", "final", "8867-4", 0, map[string]any{
				"valueString": "72 bpm",
			}),
			R5:          observationJSON("obs-type-value-string-to-quantity", "final", "8867-4", 72, nil),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.value.ofType(string).exists()", true)},
			R5JSON:      []JSONCheck{exists("valueQuantity.value"), missing("valueString")},
		},
		{
			ID: "obs-cc-category-display", ResourceType: "Observation", Category: "codeableconcept",
			R4: observationJSON("obs-cc-category-display", "final", "8480-6", 120, map[string]any{
				"category": []any{map[string]any{"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/observation-category",
					"code":   "vital-signs",
				}}}},
			}),
			R5: observationJSON("obs-cc-category-display", "final", "8480-6", 120, map[string]any{
				"category": []any{map[string]any{"coding": []any{map[string]any{
					"system":  "http://terminology.hl7.org/CodeSystem/observation-category",
					"code":    "vital-signs",
					"display": "Vital Signs",
				}}}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.category.coding.code = 'vital-signs'", true)},
			R5JSON:      []JSONCheck{equals("category.coding.display", "Vital Signs")},
		},
		{
			ID: "obs-cc-loinc-text", ResourceType: "Observation", Category: "codeableconcept",
			R4: observationJSON("obs-cc-loinc-text", "final", "8867-4", 60, nil),
			R5: observationJSON("obs-cc-loinc-text", "final", "8867-4", 60, map[string]any{
				"code": map[string]any{
					"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8867-4", "display": "Heart rate"}},
					"text":   "Heart rate",
				},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.code.coding.code = '8867-4'", true)},
			R5JSON:      []JSONCheck{equals("code.text", "Heart rate")},
		},
		{
			ID: "obs-cc-interpretation-display", ResourceType: "Observation", Category: "codeableconcept",
			R4: observationJSON("obs-cc-interpretation-display", "final", "8867-4", 40, map[string]any{
				"interpretation": []any{map[string]any{"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
					"code":   "L",
				}}}},
			}),
			R5: observationJSON("obs-cc-interpretation-display", "final", "8867-4", 40, map[string]any{
				"interpretation": []any{map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
						"code":    "L",
						"display": "Low",
					}},
					"text": "Low",
				}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Observation.interpretation.coding.code = 'L'", true)},
			R5JSON:      []JSONCheck{equals("interpretation.text", "Low")},
		},
		{
			ID: "obs-loss-note", ResourceType: "Observation", Category: "information_loss",
			R4: observationJSON("obs-loss-note", "final", "8867-4", 55, map[string]any{
				"note": []any{map[string]any{"text": "legacy device annotation"}},
			}),
			R5:              observationJSON("obs-loss-note", "final", "8867-4", 55, nil),
			StablePaths:     []string{"id", "status"},
			InformationLoss: []string{"Observation.note"},
			R4FHIRPath:      []FHIRPathAssert{fp("Observation.note.exists()", true)},
			R5JSON:          []JSONCheck{missing("note")},
			Notes:           "Illustrative drop of narrative note when a converter cannot preserve it.",
		},
		{
			ID: "obs-loss-comment-extension", ResourceType: "Observation", Category: "information_loss",
			R4: observationJSON("obs-loss-comment-extension", "final", "8310-5", 38.1, map[string]any{
				"extension": []any{map[string]any{
					"url":         "https://example.org/StructureDefinition/device-trace",
					"valueString": "therm-9",
				}},
			}),
			R5:              observationJSON("obs-loss-comment-extension", "final", "8310-5", 38.1, nil),
			StablePaths:     []string{"id", "code"},
			InformationLoss: []string{"Observation.extension[https://example.org/StructureDefinition/device-trace]"},
			R4FHIRPath:      []FHIRPathAssert{fp("Observation.extension.exists()", true)},
			R5JSON:          []JSONCheck{missing("extension")},
		},
	}
}

func conditionPairs() []Pair {
	return []Pair{
		{
			ID: "cond-identity-hypertension", ResourceType: "Condition", Category: "identity",
			R4:          conditionJSON("cond-identity-hypertension", "active", "38341003", "", nil),
			R5:          conditionJSON("cond-identity-hypertension", "active", "38341003", "", nil),
			StablePaths: []string{"id", "clinicalStatus", "code", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.clinicalStatus.coding.code = 'active'", true)},
			R5JSON:      []JSONCheck{equals("clinicalStatus.coding.code", "active"), exists("subject")},
		},
		{
			ID: "cond-identity-diabetes", ResourceType: "Condition", Category: "identity",
			R4:          conditionJSON("cond-identity-diabetes", "active", "44054006", "", nil),
			R5:          conditionJSON("cond-identity-diabetes", "active", "44054006", "", nil),
			StablePaths: []string{"id", "code", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.code.coding.code = '44054006'", true)},
			R5JSON:      []JSONCheck{equals("code.coding.code", "44054006")},
		},
		{
			ID: "cond-identity-inactive-asthma", ResourceType: "Condition", Category: "identity",
			R4:          conditionJSON("cond-identity-inactive-asthma", "inactive", "195967001", "", nil),
			R5:          conditionJSON("cond-identity-inactive-asthma", "inactive", "195967001", "", nil),
			StablePaths: []string{"id", "clinicalStatus"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.clinicalStatus.coding.code = 'inactive'", true)},
			R5JSON:      []JSONCheck{equals("clinicalStatus.coding.code", "inactive")},
		},
		{
			ID: "cond-identity-with-note", ResourceType: "Condition", Category: "identity",
			R4: conditionJSON("cond-identity-with-note", "active", "38341003", "", map[string]any{
				"note": []any{map[string]any{"text": "well controlled"}},
			}),
			R5: conditionJSON("cond-identity-with-note", "active", "38341003", "", map[string]any{
				"note": []any{map[string]any{"text": "well controlled"}},
			}),
			StablePaths: []string{"id", "note"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.note.exists()", true)},
			R5JSON:      []JSONCheck{equals("note.text", "well controlled")},
		},
		{
			ID: "cond-renamed-asserter-authen", ResourceType: "Condition", Category: "renamed",
			R4: conditionJSON("cond-renamed-asserter-authen", "active", "38341003", "Practitioner/pr-1", nil),
			R5: conditionJSON("cond-renamed-asserter-authen", "active", "38341003", "", map[string]any{
				"participant": []any{map[string]any{
					"function": map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/v3-ParticipationType",
						"code":   "AUTHEN",
					}}},
					"actor": map[string]any{"reference": "Practitioner/pr-1"},
				}},
			}),
			StablePaths: []string{"id", "code", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.asserter.exists()", true)},
			R5JSON:      []JSONCheck{exists("participant.actor.reference"), missing("asserter"), equals("participant.function.coding.code", "AUTHEN")},
			Notes:       "Condition.asserter (R4) → Condition.participant (R5).",
		},
		{
			ID: "cond-renamed-asserter-inform", ResourceType: "Condition", Category: "renamed",
			R4: conditionJSON("cond-renamed-asserter-inform", "active", "44054006", "Practitioner/pr-2", nil),
			R5: conditionJSON("cond-renamed-asserter-inform", "active", "44054006", "", map[string]any{
				"participant": []any{map[string]any{
					"function": map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/v3-ParticipationType",
						"code":   "INFORM",
					}}},
					"actor": map[string]any{"reference": "Practitioner/pr-2"},
				}},
			}),
			StablePaths: []string{"id", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.asserter.exists()", true)},
			R5JSON:      []JSONCheck{equals("participant.function.coding.code", "INFORM"), missing("asserter")},
		},
		{
			ID: "cond-renamed-asserter-patient", ResourceType: "Condition", Category: "renamed",
			R4: conditionJSON("cond-renamed-asserter-patient", "active", "195967001", "Patient/pat-1", nil),
			R5: conditionJSON("cond-renamed-asserter-patient", "active", "195967001", "", map[string]any{
				"participant": []any{map[string]any{
					"actor": map[string]any{"reference": "Patient/pat-1"},
				}},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.asserter.exists()", true)},
			R5JSON:      []JSONCheck{equals("participant.actor.reference", "Patient/pat-1"), missing("asserter")},
		},
		{
			ID: "cond-renamed-asserter-org", ResourceType: "Condition", Category: "renamed",
			R4: conditionJSON("cond-renamed-asserter-org", "active", "38341003", "PractitionerRole/prrole-1", nil),
			R5: conditionJSON("cond-renamed-asserter-org", "active", "38341003", "", map[string]any{
				"participant": []any{map[string]any{
					"actor": map[string]any{"reference": "PractitionerRole/prrole-1"},
				}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.asserter.exists()", true)},
			R5JSON:      []JSONCheck{equals("participant.actor.reference", "PractitionerRole/prrole-1"), missing("asserter")},
		},
		{
			ID: "cond-card-category-additional", ResourceType: "Condition", Category: "cardinality",
			R4: conditionJSON("cond-card-category-additional", "active", "38341003", "", map[string]any{
				"category": []any{map[string]any{"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/condition-category", "code": "problem-list-item",
				}}}},
			}),
			R5: conditionJSON("cond-card-category-additional", "active", "38341003", "", map[string]any{
				"category": []any{
					map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/condition-category", "code": "problem-list-item",
					}}},
					map[string]any{"coding": []any{map[string]any{
						"system": "http://terminology.hl7.org/CodeSystem/condition-category", "code": "encounter-diagnosis",
					}}},
				},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.category.count() = 1", true)},
			R5JSON:      []JSONCheck{count("category", 2)},
		},
		{
			ID: "cond-card-stage-assessment-additional", ResourceType: "Condition", Category: "cardinality",
			R4: conditionJSON("cond-card-stage-assessment-additional", "active", "363346000", "", map[string]any{
				"stage": []any{map[string]any{
					"assessment": []any{map[string]any{"reference": "Observation/stage-1"}},
				}},
			}),
			R5: conditionJSON("cond-card-stage-assessment-additional", "active", "363346000", "", map[string]any{
				"stage": []any{map[string]any{
					"assessment": []any{
						map[string]any{"reference": "Observation/stage-1"},
						map[string]any{"reference": "Observation/stage-2"},
					},
				}},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.stage.exists()", true)},
			R5JSON:      []JSONCheck{count("stage.assessment", 2)},
		},
		{
			ID: "cond-type-onset-datetime-to-age", ResourceType: "Condition", Category: "type",
			R4: conditionJSON("cond-type-onset-datetime-to-age", "active", "38341003", "", map[string]any{
				"onsetDateTime": "2018-03-01",
			}),
			R5: conditionJSON("cond-type-onset-datetime-to-age", "active", "38341003", "", map[string]any{
				"onsetAge": map[string]any{"value": 54, "unit": "a", "system": "http://unitsofmeasure.org", "code": "a"},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.onset.ofType(dateTime).exists()", true)},
			R5JSON:      []JSONCheck{exists("onsetAge.value"), missing("onsetDateTime")},
			Notes:       "onset[x] choice type shift dateTime → Age.",
		},
		{
			ID: "cond-type-abatement-datetime-to-string", ResourceType: "Condition", Category: "type",
			R4: conditionJSON("cond-type-abatement-datetime-to-string", "inactive", "195967001", "", map[string]any{
				"abatementDateTime": "2021-06-01",
			}),
			R5: conditionJSON("cond-type-abatement-datetime-to-string", "inactive", "195967001", "", map[string]any{
				"abatementString": "resolved in childhood",
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.abatement.ofType(dateTime).exists()", true)},
			R5JSON:      []JSONCheck{equals("abatementString", "resolved in childhood"), missing("abatementDateTime")},
		},
		{
			ID: "cond-cc-severity-display", ResourceType: "Condition", Category: "codeableconcept",
			R4: conditionJSON("cond-cc-severity-display", "active", "38341003", "", map[string]any{
				"severity": map[string]any{"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "24484000"}}},
			}),
			R5: conditionJSON("cond-cc-severity-display", "active", "38341003", "", map[string]any{
				"severity": map[string]any{
					"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "24484000", "display": "Severe"}},
					"text":   "Severe",
				},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.severity.coding.code.exists()", true)},
			R5JSON:      []JSONCheck{equals("severity.text", "Severe")},
		},
		{
			ID: "cond-cc-clinical-display", ResourceType: "Condition", Category: "codeableconcept",
			R4: conditionJSON("cond-cc-clinical-display", "active", "38341003", "", nil),
			R5: conditionJSON("cond-cc-clinical-display", "active", "38341003", "", map[string]any{
				"clinicalStatus": map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://terminology.hl7.org/CodeSystem/condition-clinical",
						"code":    "active",
						"display": "Active",
					}},
					"text": "Active",
				},
			}),
			StablePaths: []string{"id", "code"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.clinicalStatus.coding.code = 'active'", true)},
			R5JSON:      []JSONCheck{equals("clinicalStatus.text", "Active")},
		},
		{
			ID: "cond-cc-code-text", ResourceType: "Condition", Category: "codeableconcept",
			R4: conditionJSON("cond-cc-code-text", "active", "38341003", "", nil),
			R5: conditionJSON("cond-cc-code-text", "active", "38341003", "", map[string]any{
				"code": map[string]any{
					"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "38341003", "display": "Hypertension"}},
					"text":   "Hypertension",
				},
			}),
			StablePaths: []string{"id", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("Condition.code.coding.code = '38341003'", true)},
			R5JSON:      []JSONCheck{equals("code.text", "Hypertension")},
		},
		{
			ID: "cond-loss-evidence-detail", ResourceType: "Condition", Category: "information_loss",
			R4: conditionJSON("cond-loss-evidence-detail", "active", "38341003", "", map[string]any{
				"evidence": []any{map[string]any{
					"detail": []any{map[string]any{"reference": "Observation/obs-sbp-1"}},
				}},
			}),
			R5:              conditionJSON("cond-loss-evidence-detail", "active", "38341003", "", nil),
			StablePaths:     []string{"id", "code"},
			InformationLoss: []string{"Condition.evidence"},
			R4FHIRPath:      []FHIRPathAssert{fp("Condition.evidence.exists()", true)},
			R5JSON:          []JSONCheck{missing("evidence")},
			Notes:           "Condition.evidence was removed in R5.",
		},
		{
			ID: "cond-loss-evidence-code", ResourceType: "Condition", Category: "information_loss",
			R4: conditionJSON("cond-loss-evidence-code", "inactive", "38341003", "", map[string]any{
				"evidence": []any{map[string]any{"code": []any{map[string]any{"text": "chart review"}}}},
			}),
			R5:              conditionJSON("cond-loss-evidence-code", "inactive", "38341003", "", nil),
			StablePaths:     []string{"id"},
			InformationLoss: []string{"Condition.evidence"},
			R4FHIRPath:      []FHIRPathAssert{fp("Condition.evidence.exists()", true)},
			R5JSON:          []JSONCheck{missing("evidence"), equals("clinicalStatus.coding.code", "inactive")},
		},
		{
			ID: "cond-loss-evidence-two-details", ResourceType: "Condition", Category: "information_loss",
			R4: conditionJSON("cond-loss-evidence-two-details", "active", "44054006", "", map[string]any{
				"evidence": []any{map[string]any{"detail": []any{
					map[string]any{"reference": "Observation/a1c"},
					map[string]any{"reference": "Observation/glucose"},
				}}},
			}),
			R5:              conditionJSON("cond-loss-evidence-two-details", "active", "44054006", "", nil),
			StablePaths:     []string{"id", "code"},
			InformationLoss: []string{"Condition.evidence"},
			R4FHIRPath:      []FHIRPathAssert{fp("Condition.evidence.exists()", true)},
			R5JSON:          []JSONCheck{missing("evidence")},
		},
	}
}

func medicationRequestPairs() []Pair {
	return []Pair{
		{
			ID: "medreq-identity-lisinopril-active", ResourceType: "MedicationRequest", Category: "identity",
			R4:          medReqJSON("medreq-identity-lisinopril-active", "active", "314076", nil),
			R5:          medReqR5JSON("medreq-identity-lisinopril-active", "active", "314076", nil),
			StablePaths: []string{"id", "status", "intent", "subject"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.status = 'active'", true), fp("MedicationRequest.intent = 'order'", true)},
			R5JSON:      []JSONCheck{equals("status", "active"), equals("intent", "order"), exists("medication.concept"), missing("medicationCodeableConcept")},
			Notes:       "status/intent/subject are stable; R5 still uses medication CodeableReference.",
		},
		{
			ID: "medreq-identity-completed", ResourceType: "MedicationRequest", Category: "identity",
			R4:          medReqJSON("medreq-identity-completed", "completed", "197361", nil),
			R5:          medReqR5JSON("medreq-identity-completed", "completed", "197361", nil),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.status = 'completed'", true)},
			R5JSON:      []JSONCheck{equals("status", "completed"), exists("medication.concept"), missing("medicationCodeableConcept")},
		},
		{
			ID: "medreq-identity-with-dosage", ResourceType: "MedicationRequest", Category: "identity",
			R4: medReqJSON("medreq-identity-with-dosage", "active", "314076", map[string]any{
				"dosageInstruction": []any{map[string]any{"text": "5 mg oral daily"}},
			}),
			R5: medReqR5JSON("medreq-identity-with-dosage", "active", "314076", map[string]any{
				"dosageInstruction": []any{map[string]any{"text": "5 mg oral daily"}},
			}),
			StablePaths: []string{"id", "dosageInstruction"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.dosageInstruction.exists()", true)},
			R5JSON:      []JSONCheck{equals("dosageInstruction.text", "5 mg oral daily"), exists("medication.concept")},
		},
		{
			ID: "medreq-identity-requester", ResourceType: "MedicationRequest", Category: "identity",
			R4: medReqJSON("medreq-identity-requester", "active", "314076", map[string]any{
				"requester": map[string]any{"reference": "Practitioner/pr-1"},
			}),
			R5: medReqR5JSON("medreq-identity-requester", "active", "314076", map[string]any{
				"requester": map[string]any{"reference": "Practitioner/pr-1"},
			}),
			StablePaths: []string{"id", "requester"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.requester.exists()", true)},
			R5JSON:      []JSONCheck{equals("requester.reference", "Practitioner/pr-1"), exists("medication.concept")},
		},
		{
			ID: "medreq-renamed-reason-code", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-reason-code", "active", "314076", map[string]any{
				"reasonCode": []any{map[string]any{
					"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "38341003"}},
				}},
			}),
			R5: medReqR5JSON("medreq-renamed-reason-code", "active", "314076", map[string]any{
				"reason": []any{map[string]any{
					"concept": map[string]any{
						"coding": []any{map[string]any{"system": "http://snomed.info/sct", "code": "38341003"}},
					},
				}},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.reasonCode.exists()", true)},
			R5JSON:      []JSONCheck{exists("reason.concept"), missing("reasonCode")},
			Notes:       "reasonCode (R4) → reason CodeableReference.concept (R5).",
		},
		{
			ID: "medreq-renamed-reason-reference", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-reason-reference", "active", "314076", map[string]any{
				"reasonReference": []any{map[string]any{"reference": "Condition/cond-identity-hypertension"}},
			}),
			R5: medReqR5JSON("medreq-renamed-reason-reference", "active", "314076", map[string]any{
				"reason": []any{map[string]any{
					"reference": map[string]any{"reference": "Condition/cond-identity-hypertension"},
				}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.reasonReference.exists()", true)},
			R5JSON:      []JSONCheck{exists("reason.reference"), missing("reasonReference")},
		},
		{
			ID: "medreq-renamed-reason-both", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-reason-both", "active", "314076", map[string]any{
				"reasonCode":      []any{map[string]any{"text": "hypertension"}},
				"reasonReference": []any{map[string]any{"reference": "Condition/htn"}},
			}),
			R5: medReqR5JSON("medreq-renamed-reason-both", "active", "314076", map[string]any{
				"reason": []any{
					map[string]any{"concept": map[string]any{"text": "hypertension"}},
					map[string]any{"reference": map[string]any{"reference": "Condition/htn"}},
				},
			}),
			StablePaths: []string{"id", "intent"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.reasonCode.exists()", true), fp("MedicationRequest.reasonReference.exists()", true)},
			R5JSON:      []JSONCheck{count("reason", 2), missing("reasonCode"), missing("reasonReference")},
		},
		{
			ID: "medreq-renamed-reason-text", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-reason-text", "active", "314076", map[string]any{
				"reasonCode": []any{map[string]any{"text": "elevated blood pressure"}},
			}),
			R5: medReqR5JSON("medreq-renamed-reason-text", "active", "314076", map[string]any{
				"reason": []any{map[string]any{"concept": map[string]any{"text": "elevated blood pressure"}}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.reasonCode.exists()", true)},
			R5JSON:      []JSONCheck{equals("reason.concept.text", "elevated blood pressure")},
		},
		{
			ID: "medreq-renamed-reported", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-reported", "active", "314076", map[string]any{
				"reportedBoolean": true,
			}),
			R5: medReqR5JSON("medreq-renamed-reported", "active", "314076", map[string]any{
				"reported":          true,
				"informationSource": []any{map[string]any{"reference": map[string]any{"reference": "Patient/pat-1"}}},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.reported.ofType(boolean).exists()", true)},
			R5JSON:      []JSONCheck{equals("reported", true), exists("informationSource"), missing("reportedBoolean")},
			Notes:       "reported[x] (R4) → reported + informationSource (R5).",
		},
		{
			ID: "medreq-renamed-instantiates-canonical", ResourceType: "MedicationRequest", Category: "renamed",
			R4: medReqJSON("medreq-renamed-instantiates-canonical", "active", "314076", map[string]any{
				"instantiatesCanonical": []any{"https://example.org/PlanDefinition/htn-rx"},
			}),
			R5: medReqR5JSON("medreq-renamed-instantiates-canonical", "active", "314076", map[string]any{
				"basedOn": []any{map[string]any{"reference": "https://example.org/PlanDefinition/htn-rx"}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.instantiatesCanonical.exists()", true)},
			R5JSON:      []JSONCheck{exists("basedOn"), missing("instantiatesCanonical")},
			Notes:       "instantiatesCanonical (R4) is not on R5 MedicationRequest; basedOn carries the plan.",
		},
		{
			ID: "medreq-type-medication-codeableconcept-to-concept", ResourceType: "MedicationRequest", Category: "type",
			R4:          medReqJSON("medreq-type-medication-codeableconcept-to-concept", "active", "314076", nil),
			R5:          medReqR5JSON("medreq-type-medication-codeableconcept-to-concept", "active", "314076", nil),
			StablePaths: []string{"id", "status", "intent"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.medication.ofType(CodeableConcept).exists()", true)},
			R5JSON:      []JSONCheck{exists("medication.concept"), missing("medicationCodeableConcept")},
			Notes:       "medication[x] (R4) → medication CodeableReference (R5).",
		},
		{
			ID: "medreq-type-medication-reference-to-reference", ResourceType: "MedicationRequest", Category: "type",
			R4: func() json.RawMessage {
				obj := map[string]any{
					"resourceType":        "MedicationRequest",
					"id":                  "medreq-type-medication-reference-to-reference",
					"status":              "active",
					"intent":              "order",
					"subject":             map[string]any{"reference": "Patient/pat-1"},
					"medicationReference": map[string]any{"reference": "Medication/med-1"},
				}
				return researchutil.MustJSON(obj)
			}(),
			R5: medReqR5JSON("medreq-type-medication-reference-to-reference", "active", "314076", map[string]any{
				"medication": map[string]any{
					"reference": map[string]any{"reference": "Medication/med-1"},
				},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.medication.ofType(Reference).exists()", true)},
			R5JSON:      []JSONCheck{exists("medication.reference"), missing("medicationReference")},
		},
		{
			ID: "medreq-card-dosage-additional", ResourceType: "MedicationRequest", Category: "cardinality",
			R4: medReqJSON("medreq-card-dosage-additional", "active", "314076", map[string]any{
				"dosageInstruction": []any{map[string]any{"text": "5 mg oral daily"}},
			}),
			R5: medReqR5JSON("medreq-card-dosage-additional", "active", "314076", map[string]any{
				"dosageInstruction": []any{
					map[string]any{"text": "5 mg oral daily"},
					map[string]any{"text": "hold if hypotensive"},
				},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.dosageInstruction.count() = 1", true)},
			R5JSON:      []JSONCheck{count("dosageInstruction", 2), exists("medication.concept"), missing("medicationCodeableConcept")},
		},
		{
			ID: "medreq-card-identifier-additional", ResourceType: "MedicationRequest", Category: "cardinality",
			R4: medReqJSON("medreq-card-identifier-additional", "active", "314076", map[string]any{
				"identifier": []any{map[string]any{"system": "https://example.org/rx", "value": "rx-1"}},
			}),
			R5: medReqR5JSON("medreq-card-identifier-additional", "active", "314076", map[string]any{
				"identifier": []any{
					map[string]any{"system": "https://example.org/rx", "value": "rx-1"},
					map[string]any{"system": "https://example.org/erx", "value": "erx-9"},
				},
			}),
			StablePaths: []string{"id", "status"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.identifier.count() = 1", true)},
			R5JSON:      []JSONCheck{count("identifier", 2), exists("medication.concept"), missing("medicationCodeableConcept")},
		},
		{
			ID: "medreq-cc-category-display", ResourceType: "MedicationRequest", Category: "codeableconcept",
			R4: medReqJSON("medreq-cc-category-display", "active", "314076", map[string]any{
				"category": []any{map[string]any{"coding": []any{map[string]any{
					"system": "http://terminology.hl7.org/CodeSystem/medicationrequest-category", "code": "community",
				}}}},
			}),
			R5: medReqR5JSON("medreq-cc-category-display", "active", "314076", map[string]any{
				"category": []any{map[string]any{
					"coding": []any{map[string]any{
						"system":  "http://terminology.hl7.org/CodeSystem/medicationrequest-category",
						"code":    "community",
						"display": "Community",
					}},
					"text": "Community",
				}},
			}),
			StablePaths: []string{"id"},
			R4FHIRPath:  []FHIRPathAssert{fp("MedicationRequest.category.coding.code = 'community'", true)},
			R5JSON:      []JSONCheck{equals("category.text", "Community"), exists("medication.concept"), missing("medicationCodeableConcept")},
		},
		{
			ID: "medreq-loss-detected-issue", ResourceType: "MedicationRequest", Category: "information_loss",
			R4: medReqJSON("medreq-loss-detected-issue", "active", "314076", map[string]any{
				"detectedIssue": []any{map[string]any{"reference": "DetectedIssue/di-1"}},
			}),
			R5:              medReqR5JSON("medreq-loss-detected-issue", "active", "314076", nil),
			StablePaths:     []string{"id", "status"},
			InformationLoss: []string{"MedicationRequest.detectedIssue"},
			R4FHIRPath:      []FHIRPathAssert{fp("MedicationRequest.detectedIssue.exists()", true)},
			R5JSON:          []JSONCheck{missing("detectedIssue"), exists("medication.concept"), missing("medicationCodeableConcept")},
			Notes:           "detectedIssue is not present on R5 MedicationRequest.",
		},
		{
			ID: "medreq-loss-detected-issue-cancelled", ResourceType: "MedicationRequest", Category: "information_loss",
			R4: medReqJSON("medreq-loss-detected-issue-cancelled", "cancelled", "314076", map[string]any{
				"detectedIssue": []any{map[string]any{"reference": "DetectedIssue/di-2"}},
			}),
			R5:              medReqR5JSON("medreq-loss-detected-issue-cancelled", "cancelled", "314076", nil),
			StablePaths:     []string{"id"},
			InformationLoss: []string{"MedicationRequest.detectedIssue"},
			R4FHIRPath:      []FHIRPathAssert{fp("MedicationRequest.detectedIssue.exists()", true)},
			R5JSON:          []JSONCheck{missing("detectedIssue"), equals("status", "cancelled"), exists("medication.concept")},
		},
		{
			ID: "medreq-loss-instantiates-uri", ResourceType: "MedicationRequest", Category: "information_loss",
			R4: medReqJSON("medreq-loss-instantiates-uri", "active", "314076", map[string]any{
				"instantiatesUri": []any{"https://example.org/legacy-protocol"},
			}),
			R5:              medReqR5JSON("medreq-loss-instantiates-uri", "active", "314076", nil),
			StablePaths:     []string{"id", "status"},
			InformationLoss: []string{"MedicationRequest.instantiatesUri"},
			R4FHIRPath:      []FHIRPathAssert{fp("MedicationRequest.instantiatesUri.exists()", true)},
			R5JSON:          []JSONCheck{missing("instantiatesUri"), exists("medication.concept"), missing("medicationCodeableConcept")},
			Notes:           "instantiatesUri was removed from R5 MedicationRequest.",
		},
	}
}

func patientObj(id string, extra map[string]any) json.RawMessage {
	obj := map[string]any{
		"resourceType": "Patient",
		"id":           id,
	}
	for k, v := range extra {
		obj[k] = v
	}
	return researchutil.MustJSON(obj)
}

func observationJSON(id, status, code string, value float64, extra map[string]any) json.RawMessage {
	obj := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       status,
		"code": map[string]any{
			"coding": []any{map[string]any{"system": "http://loinc.org", "code": code}},
			"text":   code,
		},
		"subject": map[string]any{"reference": "Patient/pat-1"},
	}
	if extra != nil {
		if _, hasValue := extra["valueCodeableConcept"]; !hasValue {
			if _, hasString := extra["valueString"]; !hasString {
				if _, hasComponentOnly := extra["component"]; !hasComponentOnly || value != 0 {
					obj["valueQuantity"] = map[string]any{"value": value, "unit": "1", "system": "http://unitsofmeasure.org"}
				}
			}
		}
	} else {
		obj["valueQuantity"] = map[string]any{"value": value, "unit": "1", "system": "http://unitsofmeasure.org"}
	}
	for k, v := range extra {
		if k == "valueCodeableConcept" || k == "valueString" {
			delete(obj, "valueQuantity")
		}
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

func medReqR5JSON(id, status, rxnorm string, extra map[string]any) json.RawMessage {
	obj := map[string]any{
		"resourceType": "MedicationRequest",
		"id":           id,
		"status":       status,
		"intent":       "order",
		"subject":      map[string]any{"reference": "Patient/pat-1"},
		"medication": map[string]any{
			"concept": map[string]any{
				"coding": []any{map[string]any{"system": "http://www.nlm.nih.gov/research/umls/rxnorm", "code": rxnorm}},
				"text":   "Lisinopril",
			},
		},
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
