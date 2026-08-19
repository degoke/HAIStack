package auth

import (
	"fmt"
	"net/url"
)

// ErrNoPatientSearchScope is returned when a patient-scoped principal searches a
// resource type that has no configured patient relationship search parameter.
var ErrNoPatientSearchScope = fmt.Errorf("%w: no patient search scope configured", ErrDenied)

// DefaultPatientSearchParams maps common FHIR R4 resource types to the search
// parameter that scopes them to one patient. Hosts may override or extend this
// map when wiring HTTP or AI adapters.
func DefaultPatientSearchParams() map[string]string {
	return map[string]string{
		"AllergyIntolerance":     "patient",
		"Appointment":          "patient",
		"CarePlan":               "subject",
		"CareTeam":               "subject",
		"ClinicalImpression":     "subject",
		"Condition":              "subject",
		"Consent":                "patient",
		"DetectedIssue":          "patient",
		"DeviceRequest":          "subject",
		"DeviceUseStatement":     "subject",
		"DiagnosticReport":       "subject",
		"DocumentReference":      "subject",
		"Encounter":              "subject",
		"EpisodeOfCare":          "patient",
		"FamilyMemberHistory":    "patient",
		"Flag":                   "subject",
		"Goal":                   "subject",
		"ImagingStudy":           "subject",
		"Immunization":           "patient",
		"List":                   "subject",
		"Media":                  "subject",
		"MedicationAdministration": "subject",
		"MedicationDispense":     "subject",
		"MedicationRequest":      "subject",
		"MedicationStatement":    "subject",
		"NutritionOrder":         "patient",
		"Observation":            "subject",
		"Procedure":              "subject",
		"QuestionnaireResponse":  "subject",
		"RequestGroup":           "subject",
		"ResearchSubject":        "individual",
		"RiskAssessment":         "subject",
		"ServiceRequest":         "subject",
		"Specimen":               "subject",
		"SupplyDelivery":         "patient",
	}
}

// MergePatientSearchParams returns a copy of defaults with overrides applied.
func MergePatientSearchParams(overrides map[string]string) map[string]string {
	merged := DefaultPatientSearchParams()
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

// ApplyPatientSearchScopeToParams injects query-time patient filters into FHIR
// search parameters. Patient searches are narrowed to _id. Other resource types
// use the configured relationship parameter (for example subject=Patient/{id}).
func ApplyPatientSearchScopeToParams(params url.Values, resourceType, patientID string, searchParams map[string]string) (url.Values, error) {
	if patientID == "" {
		return params, nil
	}
	out := CloneURLValues(params)
	if resourceType == "Patient" {
		out.Set("_id", patientID)
		return out, nil
	}
	if searchParams == nil {
		searchParams = DefaultPatientSearchParams()
	}
	param := searchParams[resourceType]
	if param == "" {
		return nil, fmt.Errorf("%w for resource type %s", ErrNoPatientSearchScope, resourceType)
	}
	out.Set(param, "Patient/"+patientID)
	return out, nil
}

// CloneURLValues returns a shallow copy of url.Values.
func CloneURLValues(v url.Values) url.Values {
	if v == nil {
		return url.Values{}
	}
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}
