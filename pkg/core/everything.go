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
	Types []string
	Since time.Time
	Count int
}

var defaultEverythingTypes = []string{
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
		typesToScan = defaultEverythingTypes
	}
	limit := q.Count
	if limit <= 0 {
		limit = defaultEverythingCount
	}

	out := make([]*types.ResourceEnvelope, 0, 8)
	appendEnv := func(env *types.ResourceEnvelope) bool {
		if env == nil {
			return true
		}
		if !q.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(q.Since) {
			return true
		}
		out = append(out, env)
		return len(out) < limit
	}
	if !appendEnv(patient) {
		return out, nil
	}

	for _, resourceType := range typesToScan {
		if resourceType == "" || resourceType == "Patient" {
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
			if !resourceInPatientCompartment(env, patientID) {
				continue
			}
			if !appendEnv(env) {
				return out, nil
			}
		}
	}
	return out, nil
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

func resourceInPatientCompartment(env *types.ResourceEnvelope, patientID string) bool {
	if env == nil || patientID == "" {
		return false
	}
	if env.ResourceType == "Patient" {
		return env.ID == patientID
	}
	refFields := []string{"subject", "patient", "beneficiary", "individual"}
	for _, field := range refFields {
		ref, ok := env.StringField(field, "reference")
		if !ok {
			continue
		}
		if referenceMatchesPatient(ref, patientID) {
			return true
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(env.JSON, &obj); err != nil {
		return false
	}
	return jsonContainsPatientReference(obj, patientID)
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

func jsonContainsPatientReference(value any, patientID string) bool {
	switch node := value.(type) {
	case map[string]any:
		if ref, ok := node["reference"].(string); ok && referenceMatchesPatient(ref, patientID) {
			return true
		}
		for _, child := range node {
			if jsonContainsPatientReference(child, patientID) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if jsonContainsPatientReference(child, patientID) {
				return true
			}
		}
	}
	return false
}
