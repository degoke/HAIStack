package ai

import "strings"

// PHICatalog drives FHIR de-identification: deep FHIR path suffixes, passive
// element-name detection at any depth, meta.security confidentiality labels,
// and view column rules. Copy DefaultPHICatalog and extend fields for local
// profiles, or replace Executor.Config.Deidentify with a custom Deidentifier.
type PHICatalog struct {
	// ResourceElements lists top-level elements whose entire subtree is redacted.
	// Prefer GlobalPathSuffixes and ResourcePathSuffixes for precise control.
	ResourceElements map[string][]string
	// GlobalElements are top-level subtrees redacted on every resource type.
	GlobalElements []string
	// GlobalPathSuffixes match element paths at any depth (dot notation), for
	// example "telecom.value", "name.family", "identifier.value", "text.div".
	GlobalPathSuffixes []string
	// ResourcePathSuffixes adds type-specific deep path suffixes.
	ResourcePathSuffixes map[string][]string
	// PassiveSensitiveKeys overrides default leaf element names redacted at any
	// depth (family, given, value, div, ...).
	PassiveSensitiveKeys map[string]bool
	// StrictConfidentialityCodes are v3-ConfidentialityCode values that trigger
	// aggressive redaction for the whole resource (default R and V).
	StrictConfidentialityCodes []string
	// StrictSecurityLabels maps Code.system to codes that trigger strict mode.
	StrictSecurityLabels map[string][]string
	// StrictAllowPathSuffixes are paths still returned when strict mode is active
	// (for example coded status fields).
	StrictAllowPathSuffixes []string
	// ViewColumnNames lists exact column names (case-insensitive) redacted in
	// run_view row maps.
	ViewColumnNames []string
	// ViewColumnSubstrings match when contained in a column name (case-insensitive).
	ViewColumnSubstrings []string
}

// DefaultPHICatalog returns the built-in PHI profile for FHIR R4 tool output.
func DefaultPHICatalog() *PHICatalog {
	return &PHICatalog{
		ResourceElements:     defaultPHIResourceElements(),
		GlobalElements:       []string{"text", "note"},
		GlobalPathSuffixes:   defaultGlobalPathSuffixes(),
		ResourcePathSuffixes: defaultResourcePathSuffixes(),
		StrictConfidentialityCodes: []string{"R", "V", "M"},
		StrictSecurityLabels: map[string][]string{
			V3ConfidentialityCodeSystem:                          {"R", "V", "M"},
			"http://terminology.hl7.org/CodeSystem/security-labels": {"ETH", "PSY", "STD", "C", "R", "V", "EMP", "AFF", "PAT", "SDV"},
			"http://terminology.hl7.org/CodeSystem/v3-ActCode":     {"ETH", "PSY", "STD", "C", "R", "V", "EMP", "AFF", "PAT", "SDV"},
		},
		StrictAllowPathSuffixes: []string{
			"resourceType", "id", "meta.versionId", "meta.lastUpdated",
			"status", "code", "category", "clinicalStatus", "verificationStatus",
		},
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

// DefaultDeidentifier returns the standard FHIR de-identifier used when Executor
// Config.Deidentify is nil. Pass a custom Deidentifier to override.
func DefaultDeidentifier() (Deidentifier, error) {
	return NewFHIRDeidentifierWithConfig(FHIRDeidentifierConfig{
		Catalog:   DefaultPHICatalog(),
		Mode:      PHIModeStandard,
		EvalMode:  EvalModeKeywordsOnly,
		UseShared: true,
	})
}

func defaultGlobalPathSuffixes() []string {
	return []string{
		"name", "name.family", "name.given", "name.prefix", "name.suffix", "name.text", "name.period",
		"telecom", "telecom.value", "telecom.extension",
		"address", "address.line", "address.city", "address.state", "address.postalCode",
		"address.country", "address.district", "address.text", "address.period",
		"identifier", "identifier.value", "identifier.assigner", "identifier.system",
		"contact", "contact.name", "contact.telecom", "contact.address",
		"communication", "communication.language",
		"photo", "photo.data", "photo.url",
		"text", "text.div", "text.status",
		"note", "note.text", "note.time", "note.author",
		"author", "authorString", "authenticator", "custodian",
		"subject", "patient", "recipient", "sender", "performer", "actor",
		"reference", "reference.display",
		"display", "description", "comment", "conclusion", "patientInstruction",
		"content", "content.data", "content.url", "content.title",
		"payload", "payload.content", "presentedForm",
		"valueString", "valueHumanName", "valueAddress", "valueContactPoint",
		"valueReference", "valueAttachment", "valueDate", "valueDateTime", "valueTime",
		"valueInstant", "valueUri", "valueUrl", "valuePeriod", "valueQuantity",
		"birthDate", "birthdate", "deceasedDateTime", "deceasedBoolean",
		"extension.valueString", "extension.valueHumanName", "extension.valueAddress",
		"extension.valueContactPoint", "extension.valueReference", "extension.valueAttachment",
		"extension.valueDate", "extension.valueDateTime", "extension.valueTime",
		"extension.valueInstant", "extension.valueUri", "extension.valueUrl",
		"extension.valueIdentifier", "extension.valueCodeableConcept.text",
		"link", "managingOrganization", "generalPractitioner", "guarantor", "payor",
		"policyHolder", "beneficiary", "subscriber", "subscriberId",
	}
}

func defaultResourcePathSuffixes() map[string][]string {
	return map[string][]string{
		"Observation": {
			"component.valueString", "component.valueTime", "component.valueDateTime",
			"valueString", "valueTime", "valueDateTime", "valuePeriod",
		},
		"DocumentReference": {
			"context.related", "context.sourcePatientInfo",
		},
		"DiagnosticReport": {
			"imagingStudy", "media", "result",
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

var defaultPassiveSensitiveKeys = map[string]bool{
	"family": true, "given": true, "prefix": true, "suffix": true,
	"div": true, "line": true, "city": true, "state": true, "postalCode": true,
	"country": true, "district": true, "birthDate": true, "data": true, "url": true,
	"title": true, "display": true, "patientInstruction": true, "comment": true,
	"description": true, "conclusion": true, "valueString": true, "valueHumanName": true,
	"valueAddress": true, "valueContactPoint": true, "valueReference": true,
	"valueAttachment": true, "valueDate": true, "valueDateTime": true, "valueTime": true,
	"valueInstant": true, "valueUri": true, "valueUrl": true, "valuePeriod": true,
	"valueQuantity": true, "valueIdentifier": true,
	"text": true, // CodeableConcept.text, Narrative sibling keys handled via paths
	"value": true,
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
