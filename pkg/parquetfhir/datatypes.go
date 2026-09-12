package parquetfhir

import "strings"

// lookupDatatypeField returns the FHIR type for a child field within a known complex type.
func lookupDatatypeField(parentType, fieldName string) string {
	if parentType == "" || fieldName == "" {
		return ""
	}
	fields, ok := knownDatatypeFields[parentType]
	if !ok {
		return ""
	}
	if typ, ok := fields[fieldName]; ok {
		return typ
	}
	return ""
}

// inferExtensionValueType maps Extension value[x] field names to FHIR types.
func inferExtensionValueType(fieldName string) string {
	if !strings.HasPrefix(fieldName, "value") || len(fieldName) <= len("value") {
		return ""
	}
	suffix := fieldName[len("value"):]
	if typ, ok := extensionValueTypes[suffix]; ok {
		return typ
	}
	return ""
}

var extensionValueTypes = map[string]string{
	"Base64Binary":       "base64Binary",
	"Boolean":            "boolean",
	"Canonical":          "canonical",
	"Code":               "code",
	"Date":               "date",
	"DateTime":           "dateTime",
	"Decimal":            "decimal",
	"Id":                 "id",
	"Instant":            "instant",
	"Integer":            "integer",
	"Markdown":           "markdown",
	"Oid":                "oid",
	"PositiveInt":        "positiveInt",
	"String":             "string",
	"Time":               "time",
	"UnsignedInt":        "unsignedInt",
	"Uri":                "uri",
	"Url":                "url",
	"Uuid":               "uuid",
	"Address":            "Address",
	"Age":                "Quantity",
	"Annotation":         "Annotation",
	"Attachment":         "Attachment",
	"CodeableConcept":    "CodeableConcept",
	"Coding":             "Coding",
	"ContactPoint":       "ContactPoint",
	"Count":              "Quantity",
	"Distance":           "Quantity",
	"Duration":           "Quantity",
	"HumanName":          "HumanName",
	"Identifier":         "Identifier",
	"Money":              "Money",
	"Period":             "Period",
	"Quantity":           "Quantity",
	"Range":              "Range",
	"Ratio":              "Ratio",
	"Reference":          "Reference",
	"SampledData":        "SampledData",
	"Signature":          "Signature",
	"Timing":             "Timing",
	"ContactDetail":      "ContactDetail",
	"Contributor":        "Contributor",
	"DataRequirement":    "DataRequirement",
	"Expression":         "Expression",
	"ParameterDefinition": "ParameterDefinition",
	"RelatedArtifact":    "RelatedArtifact",
	"TriggerDefinition":  "TriggerDefinition",
	"UsageContext":       "UsageContext",
	"Dosage":             "Dosage",
	"Meta":               "Meta",
}

var knownDatatypeFields = map[string]map[string]string{
	"Extension": {
		"url": "uri",
	},
	"Period": {
		"start": "dateTime",
		"end":   "dateTime",
	},
	"Coding": {
		"system":  "uri",
		"code":    "code",
		"display": "string",
		"version": "string",
		"userSelected": "boolean",
	},
	"CodeableConcept": {
		"text": "string",
	},
	"Quantity": {
		"value":  "decimal",
		"unit":   "string",
		"system": "uri",
		"code":   "code",
	},
	"Reference": {
		"reference": "string",
		"type":      "code",
		"display":   "string",
		"identifier": "Identifier",
	},
	"HumanName": {
		"use":    "code",
		"text":   "string",
		"family": "string",
		"prefix": "string",
		"suffix": "string",
	},
	"ContactPoint": {
		"system": "code",
		"value":  "string",
		"use":    "code",
		"rank":   "positiveInt",
	},
	"Identifier": {
		"use":    "code",
		"type":   "CodeableConcept",
		"system": "uri",
		"value":  "string",
	},
	"Address": {
		"use":        "code",
		"type":       "code",
		"text":       "string",
		"line":       "string",
		"city":       "string",
		"state":      "string",
		"postalCode": "string",
		"country":    "string",
	},
	"Organization": {
		"id":   "id",
		"name": "string",
	},
}
