package structuremap

// isFHIRResourceType reports whether typeName names a FHIR resource (not a datatype).
func isFHIRResourceType(typeName string) bool {
	if typeName == "" {
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
		return false
	}
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

// repeatingFields lists FHIR JSON fields that are arrays for a given resource or datatype.
var repeatingFields = map[string]map[string]bool{
	"Patient": {
		"name": true, "identifier": true, "telecom": true, "address": true,
		"contact": true, "communication": true, "generalPractitioner": true, "link": true,
	},
	"Observation": {
		"identifier": true, "basedOn": true, "partOf": true, "category": true,
		"code": true, "note": true, "performer": true, "valueQuantity": true,
		"interpretation": true, "bodySite": true, "method": true, "specimen": true,
		"device": true, "referenceRange": true, "hasMember": true, "derivedFrom": true,
		"component": true,
	},
	"Bundle": {
		"entry": true, "link": true, "signature": true,
	},
	"RelatedPerson": {
		"identifier": true, "relationship": true, "name": true, "telecom": true,
		"address": true, "communication": true,
	},
	"HumanName": {
		"given": true, "prefix": true, "suffix": true,
	},
	"CodeableConcept": {
		"coding": true,
	},
}

func isRepeatingField(parent map[string]any, field string) bool {
	if parent == nil || field == "" {
		return false
	}
	resourceType, _ := parent["resourceType"].(string)
	if resourceType != "" {
		if fields, ok := repeatingFields[resourceType]; ok && fields[field] {
			return true
		}
	}
	if fields, ok := repeatingFields[field]; ok {
		// Nested datatype keys like HumanName.given are keyed by parent shape.
		_ = fields
	}
	// HumanName and CodeableConcept do not carry resourceType; infer from context.
	if field == "given" || field == "prefix" || field == "suffix" || field == "coding" {
		return true
	}
	return false
}
