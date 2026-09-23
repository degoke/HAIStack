package ai

import "strings"

// PHICatalog lists FHIR element paths and view column names treated as PHI for
// built-in de-identification. It is a conservative, operator-curated allow-list
// of sensitive fields—not a full HIPAA Safe Harbor engine. Callers can copy
// DefaultPHICatalog and extend ResourceElements or ViewColumnNames for local
// profiles.
type PHICatalog struct {
	// ResourceElements maps FHIR resource type to top-level JSON element names
	// removed before model context is returned.
	ResourceElements map[string][]string
	// GlobalElements are removed from every resource when present at the top level.
	GlobalElements []string
	// ViewColumnNames lists exact column names (case-insensitive) redacted in
	// run_view row maps.
	ViewColumnNames []string
	// ViewColumnSubstrings match when contained in a column name (case-insensitive).
	ViewColumnSubstrings []string
}

// DefaultPHICatalog returns a starter catalog for common R4 demographics and
// free-text clinical fields used in AI tool output.
func DefaultPHICatalog() *PHICatalog {
	return &PHICatalog{
		ResourceElements: defaultPHIResourceElements(),
		GlobalElements:   []string{"text", "note"},
		ViewColumnNames: []string{
			"name", "family", "given", "phone", "email", "telecom",
			"address", "birthdate", "birth_date", "dateofbirth", "date_of_birth",
			"mrn", "ssn", "identifier", "postal_code", "postalcode", "zip",
			"city", "state", "line", "photo", "contact",
		},
		ViewColumnSubstrings: []string{
			"_name", "_phone", "_email", "_address", "_birth", "_mrn", "_ssn",
			"_telecom", "_identifier", "_postal", "_zip",
		},
	}
}

func defaultPHIResourceElements() map[string][]string {
	return map[string][]string{
		"Patient": {
			"identifier", "name", "telecom", "address", "birthDate", "photo",
			"contact", "communication", "generalPractitioner", "link", "managingOrganization",
		},
		"Practitioner": {
			"identifier", "name", "telecom", "address", "photo", "qualification", "communication",
		},
		"RelatedPerson": {
			"identifier", "name", "telecom", "address", "photo", "birthDate", "patient",
		},
		"Person": {
			"identifier", "name", "telecom", "address", "photo", "birthDate", "link",
		},
		"PractitionerRole": {
			"telecom", "location", "endpoint",
		},
		"Appointment": {
			"comment", "description", "patientInstruction", "reasonReference", "supportingInformation",
		},
		"Encounter": {
			"reasonReference", "diagnosis", "hospitalization", "location", "serviceProvider",
		},
		"Observation": {
			"valueString", "valueTime", "valueDateTime", "valuePeriod", "component",
		},
		"Condition": {
			"evidence", "stage",
		},
		"AllergyIntolerance": {
			"reaction",
		},
		"DocumentReference": {
			"content", "context", "author", "authenticator", "custodian",
		},
		"DiagnosticReport": {
			"presentedForm", "conclusion", "imagingStudy", "media", "performer",
		},
		"Media": {
			"content", "device", "operator",
		},
		"Communication": {
			"payload", "sender", "recipient", "topic",
		},
		"CarePlan": {
			"addresses", "activity", "contributor", "careTeam",
		},
		"Immunization": {
			"reaction", "performer", "location",
		},
		"Procedure": {
			"focalDevice", "usedReference", "performer", "location",
		},
		"ServiceRequest": {
			"patientInstruction", "performer", "location", "supportingInfo",
		},
		"Specimen": {
			"collection", "container", "parent",
		},
		"Coverage": {
			"subscriber", "subscriberId", "beneficiary", "payor", "policyHolder",
		},
		"Claim": {
			"patient", "provider", "insurer", "careTeam", "insurance", "item", "total",
		},
		"ExplanationOfBenefit": {
			"patient", "provider", "insurer", "careTeam", "insurance", "item", "total",
		},
		"Account": {
			"subject", "owner", "guarantor", "coverage",
		},
	}
}

// ElementsForResource returns top-level element names to redact for resourceType,
// including GlobalElements.
func (c *PHICatalog) ElementsForResource(resourceType string) []string {
	if c == nil {
		c = DefaultPHICatalog()
	}
	seen := make(map[string]struct{})
	var out []string
	for _, el := range c.GlobalElements {
		if el == "" {
			continue
		}
		if _, ok := seen[el]; ok {
			continue
		}
		seen[el] = struct{}{}
		out = append(out, el)
	}
	if c.ResourceElements != nil {
		for _, el := range c.ResourceElements[resourceType] {
			if el == "" {
				continue
			}
			if _, ok := seen[el]; ok {
				continue
			}
			seen[el] = struct{}{}
			out = append(out, el)
		}
	}
	return out
}

// ViewColumnIsPHI reports whether a view column name should be redacted.
func (c *PHICatalog) ViewColumnIsPHI(column string) bool {
	if c == nil {
		c = DefaultPHICatalog()
	}
	col := strings.ToLower(strings.TrimSpace(column))
	if col == "" {
		return false
	}
	for _, name := range c.ViewColumnNames {
		if col == strings.ToLower(name) {
			return true
		}
	}
	for _, sub := range c.ViewColumnSubstrings {
		if sub != "" && strings.Contains(col, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
