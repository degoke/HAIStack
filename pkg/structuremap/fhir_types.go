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

var repeatingDatatypeFields = map[string]bool{
	"given": true, "prefix": true, "suffix": true, "coding": true,
}

func isRepeatingDatatypeField(field string) bool {
	return repeatingDatatypeFields[field]
}
