package r4

// NewId returns a FHIR id element, or nil when value is empty.
func NewId(value string) *Id {
	if value == "" {
		return nil
	}
	return &Id{Value: value}
}

// NewString returns a FHIR string element, or nil when value is empty.
func NewString(value string) *String {
	if value == "" {
		return nil
	}
	return &String{Value: value}
}

// NewCodeableConcept returns a CodeableConcept with optional display text.
func NewCodeableConcept(text string) *CodeableConcept {
	if text == "" {
		return &CodeableConcept{}
	}
	return &CodeableConcept{Text: NewString(text)}
}

// NewPatient returns a Patient with the given logical id when id is non-empty.
func NewPatient(id string) *Patient {
	patient := &Patient{}
	if id != "" {
		patient.Id = NewId(id)
	}
	return patient
}

// NewObservation returns an Observation with optional id and code text.
// Status defaults to final when codeText is set so the resource is usable in common paths.
func NewObservation(id, codeText string) *Observation {
	observation := &Observation{}
	if id != "" {
		observation.Id = NewId(id)
	}
	if codeText != "" {
		observation.Code = NewCodeableConcept(codeText)
		observation.Status = &Observation_StatusCode{Value: ObservationStatusCode_FINAL}
	}
	return observation
}
