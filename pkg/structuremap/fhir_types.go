package structuremap

import "strings"

var fhirDatatypes = map[string]bool{
	"Coding": true, "CodeableConcept": true, "HumanName": true, "Quantity": true,
	"Reference": true, "Identifier": true, "ContactPoint": true, "Period": true,
	"Address": true, "Annotation": true, "Range": true, "Ratio": true,
	"SampledData": true, "Timing": true, "Dosage": true, "Attachment": true,
	"Money": true, "Distance": true, "Duration": true, "Age": true, "Count": true,
	"BundleEntry": true, "BackboneElement": true,
}

// isFHIRResourceType reports whether typeName names a FHIR resource (not a datatype).
func isFHIRResourceType(typeName string) bool {
	typeName = resourceTypeName(typeName)
	if typeName == "" || isFHIRDatatype(typeName) {
		return false
	}
	switch typeName {
	case "Bundle", "Patient", "Observation", "RelatedPerson", "Questionnaire",
		"QuestionnaireResponse", "Parameters", "Practitioner", "Organization",
		"Condition", "Procedure", "Medication", "MedicationRequest", "Encounter",
		"DiagnosticReport", "DocumentReference", "Task", "ServiceRequest",
		"Immunization", "AllergyIntolerance", "CarePlan", "Goal", "Device":
		return true
	default:
		return looksLikeResourceType(typeName)
	}
}

func isFHIRDatatype(typeName string) bool {
	return fhirDatatypes[typeName]
}

func looksLikeResourceType(typeName string) bool {
	if typeName == "" || strings.Contains(typeName, ".") {
		return false
	}
	first := typeName[0]
	return first >= 'A' && first <= 'Z'
}

func newTypedInstance(typeName string) map[string]any {
	typeName = resourceTypeName(typeName)
	if typeName == "" {
		return map[string]any{}
	}
	if isFHIRResourceType(typeName) {
		return map[string]any{"resourceType": typeName}
	}
	return map[string]any{}
}

// repeatingFields lists FHIR JSON fields that are arrays for a given resource type.
var repeatingFields = map[string]map[string]bool{
	"Patient": {
		"name": true, "identifier": true, "telecom": true, "address": true,
		"contact": true, "communication": true, "generalPractitioner": true, "link": true,
	},
	"Observation": {
		"identifier": true, "basedOn": true, "partOf": true, "category": true,
		"note": true, "performer": true, "interpretation": true, "bodySite": true,
		"method": true, "specimen": true, "device": true, "referenceRange": true,
		"hasMember": true, "derivedFrom": true, "component": true,
	},
	"Bundle": {
		"entry": true, "link": true, "signature": true,
	},
	"RelatedPerson": {
		"identifier": true, "relationship": true, "name": true, "telecom": true,
		"address": true, "communication": true,
	},
}

var repeatingDatatypeFields = map[string]bool{
	"given": true, "prefix": true, "suffix": true, "coding": true,
}

func isRepeatingFieldHeuristic(parent map[string]any, field string) bool {
	if parent == nil || field == "" {
		return false
	}
	resourceType, _ := parent["resourceType"].(string)
	if resourceType != "" {
		if fields, ok := repeatingFields[resourceType]; ok && fields[field] {
			return true
		}
	}
	return repeatingDatatypeFields[field]
}
