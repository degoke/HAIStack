package core

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// EverythingQuery configures Patient/$everything collection.
type EverythingQuery struct {
	Types  []string
	Since  time.Time // resource meta.lastUpdated (_since)
	Start  time.Time // care-date lower bound (start)
	End    time.Time // care-date upper bound (end)
	Count  int
	Offset int
}

// DefaultEverythingTypes is the Patient compartment type list used when _type is omitted.
var DefaultEverythingTypes = []string{
	"Patient",
	"Observation",
	"Encounter",
	"Condition",
	"Procedure",
	"MedicationRequest",
	"MedicationStatement",
	"AllergyIntolerance",
	"Immunization",
	"DiagnosticReport",
	"DocumentReference",
	"Appointment",
	"CarePlan",
	"Goal",
	"ServiceRequest",
	"QuestionnaireResponse",
	"RelatedPerson",
	"Coverage",
	"Claim",
	"ExplanationOfBenefit",
	"Task",
	"Provenance",
}

const defaultEverythingCount = 1000

// patientCompartmentSearchParam is the primary FHIR R4 Patient-compartment search
// parameter for each resource type. Types without an entry are not scanned.
var patientCompartmentSearchParam = map[string]string{
	"Account":                     "subject",
	"AdverseEvent":                "subject",
	"AllergyIntolerance":          "patient",
	"Appointment":                 "actor",
	"AppointmentResponse":         "actor",
	"AuditEvent":                  "patient",
	"Basic":                       "patient",
	"BodyStructure":               "patient",
	"CarePlan":                    "patient",
	"CareTeam":                    "patient",
	"ChargeItem":                  "subject",
	"Claim":                       "patient",
	"ClaimResponse":               "patient",
	"ClinicalImpression":          "subject",
	"Communication":               "subject",
	"CommunicationRequest":        "subject",
	"Composition":                 "subject",
	"Condition":                   "patient",
	"Consent":                     "patient",
	"Coverage":                    "beneficiary",
	"CoverageEligibilityRequest":  "patient",
	"CoverageEligibilityResponse": "patient",
	"DetectedIssue":               "patient",
	"DeviceRequest":               "subject",
	"DeviceUseStatement":          "subject",
	"DiagnosticReport":            "subject",
	"DocumentManifest":            "subject",
	"DocumentReference":           "subject",
	"Encounter":                   "patient",
	"EnrollmentRequest":           "subject",
	"EpisodeOfCare":               "patient",
	"ExplanationOfBenefit":        "patient",
	"FamilyMemberHistory":         "patient",
	"Flag":                        "patient",
	"Goal":                        "patient",
	"Group":                       "member",
	"ImagingStudy":                "patient",
	"Immunization":                "patient",
	"ImmunizationEvaluation":      "patient",
	"ImmunizationRecommendation":  "patient",
	"Invoice":                     "subject",
	"List":                        "subject",
	"MeasureReport":               "patient",
	"Media":                       "subject",
	"MedicationAdministration":    "patient",
	"MedicationDispense":          "patient",
	"MedicationRequest":           "subject",
	"MedicationStatement":         "subject",
	"MolecularSequence":           "patient",
	"NutritionOrder":              "patient",
	"Observation":                 "subject",
	"Person":                      "patient",
	"Procedure":                   "patient",
	"Provenance":                  "patient",
	"QuestionnaireResponse":       "subject",
	"RelatedPerson":               "patient",
	"RequestGroup":                "subject",
	"ResearchSubject":             "individual",
	"RiskAssessment":              "subject",
	"Schedule":                    "actor",
	"ServiceRequest":              "subject",
	"Specimen":                    "subject",
	"SupplyDelivery":              "patient",
	"SupplyRequest":               "requester",
	"Task":                        "patient",
	"VisionPrescription":          "patient",
}

// patientCompartmentFields is the FHIR R4 Patient compartment reference paths
// (top-level or dotted) used to decide membership. Nested mentions outside
// these paths are not in the compartment.
var patientCompartmentFields = map[string][]string{
	"Account":                     {"subject"},
	"AdverseEvent":                {"subject"},
	"AllergyIntolerance":          {"patient"},
	"Appointment":                 {"participant.actor"},
	"AppointmentResponse":         {"actor"},
	"AuditEvent":                  {"agent.who", "entity.what"},
	"Basic":                       {"subject"},
	"BodyStructure":               {"patient"},
	"CarePlan":                    {"subject"},
	"CareTeam":                    {"subject", "participant.member"},
	"ChargeItem":                  {"subject"},
	"Claim":                       {"patient"},
	"ClaimResponse":               {"patient"},
	"ClinicalImpression":          {"subject"},
	"Communication":               {"subject"},
	"CommunicationRequest":        {"subject"},
	"Composition":                 {"subject"},
	"Condition":                   {"subject"},
	"Consent":                     {"patient"},
	"Coverage":                    {"beneficiary", "subscriber", "policyHolder"},
	"CoverageEligibilityRequest":  {"patient"},
	"CoverageEligibilityResponse": {"patient"},
	"DetectedIssue":               {"patient"},
	"DeviceRequest":               {"subject"},
	"DeviceUseStatement":          {"subject"},
	"DiagnosticReport":            {"subject"},
	"DocumentManifest":            {"subject", "author"},
	"DocumentReference":           {"subject", "author"},
	"Encounter":                   {"subject"},
	"EnrollmentRequest":           {"candidate"},
	"EpisodeOfCare":               {"patient"},
	"ExplanationOfBenefit":        {"patient"},
	"FamilyMemberHistory":         {"patient"},
	"Flag":                        {"subject"},
	"Goal":                        {"subject"},
	"Group":                       {"member.entity"},
	"ImagingStudy":                {"subject"},
	"Immunization":                {"patient"},
	"ImmunizationEvaluation":      {"patient"},
	"ImmunizationRecommendation":  {"patient"},
	"Invoice":                     {"subject", "recipient"},
	"List":                        {"subject", "source"},
	"MeasureReport":               {"subject"},
	"Media":                       {"subject"},
	"MedicationAdministration":    {"subject", "performer.actor"},
	"MedicationDispense":          {"subject", "performer.actor"},
	"MedicationRequest":           {"subject"},
	"MedicationStatement":         {"subject"},
	"MolecularSequence":           {"patient"},
	"NutritionOrder":              {"patient"},
	"Observation":                 {"subject", "performer"},
	"Person":                      {"link.target"},
	"Procedure":                   {"subject", "performer.actor"},
	"Provenance":                  {"target", "agent.who"},
	"QuestionnaireResponse":       {"subject", "author"},
	"RelatedPerson":               {"patient"},
	"RequestGroup":                {"subject"},
	"ResearchSubject":             {"individual"},
	"RiskAssessment":              {"subject"},
	"Schedule":                    {"actor"},
	"ServiceRequest":              {"subject", "performer"},
	"Specimen":                    {"subject"},
	"SupplyDelivery":              {"patient"},
	"SupplyRequest":               {"requester"},
	"Task":                        {"for", "owner", "requester"},
	"VisionPrescription":          {"patient"},
}

// PatientCompartmentSearchParam returns the primary search parameter that scopes
// resourceType to one Patient, or empty if the type is not in the compartment.
func PatientCompartmentSearchParam(resourceType string) string {
	return patientCompartmentSearchParam[resourceType]
}

// PatientCompartmentSearchParams returns every Patient-compartment search
// parameter for resourceType (for example Observation subject and performer).
func PatientCompartmentSearchParams(resourceType string) []string {
	primary := patientCompartmentSearchParam[resourceType]
	if primary == "" {
		return nil
	}
	out := []string{primary}
	seen := map[string]bool{primary: true}
	for _, extra := range patientCompartmentSearchParamExtras[resourceType] {
		if extra == "" || seen[extra] {
			continue
		}
		seen[extra] = true
		out = append(out, extra)
	}
	return out
}

// Extra compartment search parameters beyond the primary code. OR'd with the
// primary when walking Patient/$everything via search.
var patientCompartmentSearchParamExtras = map[string][]string{
	"Coverage":                 {"subscriber", "policy-holder"},
	"DocumentManifest":         {"author", "recipient"},
	"DocumentReference":        {"author"},
	"List":                     {"source"},
	"MedicationAdministration": {"performer"},
	"MedicationDispense":       {"performer"},
	"Observation":              {"performer"},
	"Procedure":                {"performer"},
	"Provenance":               {"agent"},
	"QuestionnaireResponse":    {"author"},
	"ServiceRequest":           {"performer"},
	"Task":                     {"owner", "requester"},
}

// Everything returns the Patient and resources in that patient's compartment.
func (s *ResourceService) Everything(ctx context.Context, patientID string, q EverythingQuery) ([]*types.ResourceEnvelope, error) {
	if patientID == "" {
		return nil, invalidErr("patient id is required", nil)
	}
	patient, err := s.Read(ctx, "Patient", patientID)
	if err != nil {
		return nil, err
	}
	typesToScan := q.Types
	if len(typesToScan) == 0 {
		typesToScan = DefaultEverythingTypes
	}
	limit := q.Count
	if limit <= 0 {
		limit = defaultEverythingCount
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	// Fetch one extra so callers can detect a next page without a second scan.
	need := offset + limit + 1

	out := make([]*types.ResourceEnvelope, 0, 8)
	appendEnv := func(env *types.ResourceEnvelope) bool {
		if env == nil || !resourceMatchesEverythingQuery(env, patientID, q) {
			return true
		}
		out = append(out, env)
		return len(out) < need
	}
	if !appendEnv(patient) {
		return paginateEverything(out, offset, limit), nil
	}

	for _, resourceType := range typesToScan {
		if resourceType == "" || resourceType == "Patient" {
			continue
		}
		if _, ok := patientCompartmentFields[resourceType]; !ok {
			continue
		}
		ids, err := s.listAllIDs(ctx, resourceType)
		if err != nil {
			return nil, exceptionErr("list resources for $everything", err)
		}
		for _, id := range ids {
			env, err := s.Read(ctx, resourceType, id)
			if err != nil {
				if IsNotFound(err) {
					continue
				}
				return nil, err
			}
			if !appendEnv(env) {
				return paginateEverything(out, offset, limit), nil
			}
		}
	}
	return paginateEverything(out, offset, limit), nil
}

func paginateEverything(envs []*types.ResourceEnvelope, offset, limit int) []*types.ResourceEnvelope {
	if offset >= len(envs) {
		return nil
	}
	envs = envs[offset:]
	if limit > 0 && len(envs) > limit {
		return envs[:limit]
	}
	return envs
}

func (s *ResourceService) listAllIDs(ctx context.Context, resourceType string) ([]string, error) {
	var all []string
	pageSize := 100
	for {
		ids, err := s.resources.ListIDs(ctx, resourceType, pageSize, len(all))
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		all = append(all, ids...)
		if len(ids) < pageSize {
			break
		}
	}
	return all, nil
}

func resourceMatchesEverythingQuery(env *types.ResourceEnvelope, patientID string, q EverythingQuery) bool {
	if env == nil {
		return false
	}
	if !resourceInPatientCompartment(env, patientID) {
		return false
	}
	if !q.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(q.Since) {
		return false
	}
	if q.Start.IsZero() && q.End.IsZero() {
		return true
	}
	dates := resourceCareDates(env)
	if len(dates) == 0 {
		return true
	}
	for _, ts := range dates {
		if !q.Start.IsZero() && ts.Before(q.Start) {
			continue
		}
		if !q.End.IsZero() && ts.After(q.End) {
			continue
		}
		return true
	}
	return false
}

func resourceInPatientCompartment(env *types.ResourceEnvelope, patientID string) bool {
	if env == nil || patientID == "" {
		return false
	}
	if env.ResourceType == "Patient" {
		return env.ID == patientID
	}
	fields := patientCompartmentFields[env.ResourceType]
	if len(fields) == 0 {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(env.JSON, &obj); err != nil {
		return false
	}
	for _, field := range fields {
		if jsonPathReferencesPatient(obj, field, patientID) {
			return true
		}
	}
	return false
}

func jsonPathReferencesPatient(value any, path, patientID string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	name, rest, _ := strings.Cut(path, ".")
	switch node := value.(type) {
	case map[string]any:
		child, ok := node[name]
		if !ok {
			return false
		}
		if rest == "" {
			return jsonNodeReferencesPatient(child, patientID)
		}
		return jsonPathReferencesPatient(child, rest, patientID)
	case []any:
		for _, item := range node {
			if jsonPathReferencesPatient(item, path, patientID) {
				return true
			}
		}
	}
	return false
}

func jsonNodeReferencesPatient(value any, patientID string) bool {
	switch node := value.(type) {
	case map[string]any:
		if ref, ok := node["reference"].(string); ok {
			return referenceMatchesPatient(ref, patientID)
		}
		for key, child := range node {
			if key == "contained" || key == "text" || key == "extension" || key == "modifierExtension" {
				continue
			}
			if jsonNodeReferencesPatient(child, patientID) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if jsonNodeReferencesPatient(child, patientID) {
				return true
			}
		}
	case string:
		return referenceMatchesPatient(node, patientID)
	}
	return false
}

func referenceMatchesPatient(reference, patientID string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" || patientID == "" {
		return false
	}
	want := "Patient/" + patientID
	if reference == want {
		return true
	}
	return strings.HasSuffix(reference, "/"+want)
}

func resourceCareDates(env *types.ResourceEnvelope) []time.Time {
	if env == nil || len(env.JSON) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(env.JSON, &obj); err != nil {
		return nil
	}
	var out []time.Time
	collectCareDates(obj, &out)
	return out
}

func collectCareDates(value any, out *[]time.Time) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			switch key {
			case "effectiveDateTime", "issued", "date", "authoredOn", "recordedDate",
				"occurrenceDateTime", "onsetDateTime", "performedDateTime", "created":
				if ts, ok := parseCareDate(child); ok {
					*out = append(*out, ts)
				}
			case "effectivePeriod", "period", "onsetPeriod", "performedPeriod", "occurrencePeriod", "billablePeriod":
				collectPeriodDates(child, out)
			default:
				if key == "meta" || key == "text" || key == "extension" {
					continue
				}
				collectCareDates(child, out)
			}
		}
	case []any:
		for _, child := range node {
			collectCareDates(child, out)
		}
	}
}

func collectPeriodDates(value any, out *[]time.Time) {
	switch node := value.(type) {
	case map[string]any:
		if ts, ok := parseCareDate(node["start"]); ok {
			*out = append(*out, ts)
		}
		if ts, ok := parseCareDate(node["end"]); ok {
			*out = append(*out, ts)
		}
	case []any:
		for _, child := range node {
			collectPeriodDates(child, out)
		}
	}
}

func parseCareDate(value any) (time.Time, bool) {
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}
