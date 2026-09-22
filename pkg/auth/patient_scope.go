package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/degoke/haistack/pkg/types"
)

// ErrNoPatientSearchScope is returned when a patient-scoped principal searches a
// resource type that has no configured patient relationship search parameter.
var ErrNoPatientSearchScope = fmt.Errorf("%w: no patient search scope configured", ErrDenied)

// PatientSearchParamResolver resolves the FHIR search parameter code that scopes
// one resource type to a single Patient reference. Registry snapshots implement
// this from installed SearchParameters; maps can be used for tests and overrides.
type PatientSearchParamResolver interface {
	PatientSearchParameterCode(resourceType string) (code string, ok bool)
}

// MapPatientSearchParamResolver adapts a static map for tests and host overrides.
type MapPatientSearchParamResolver map[string]string

func (m MapPatientSearchParamResolver) PatientSearchParameterCode(resourceType string) (string, bool) {
	if m == nil {
		return "", false
	}
	code, ok := m[resourceType]
	return code, ok
}

// OverridePatientSearchParamResolver applies map overrides before delegating to base.
type OverridePatientSearchParamResolver struct {
	Base      PatientSearchParamResolver
	Overrides MapPatientSearchParamResolver
}

func (r OverridePatientSearchParamResolver) PatientSearchParameterCode(resourceType string) (string, bool) {
	if code, ok := r.Overrides.PatientSearchParameterCode(resourceType); ok {
		return code, true
	}
	if r.Base != nil {
		return r.Base.PatientSearchParameterCode(resourceType)
	}
	return "", false
}

// ApplyPatientSearchScopeToParams injects query-time patient filters into FHIR
// search parameters so the store never returns out-of-compartment primary
// matches. Patient searches are narrowed to _id. Other resource types use the
// resolved relationship parameter (for example subject=Patient/{id}).
func ApplyPatientSearchScopeToParams(params url.Values, resourceType, patientID string, resolver PatientSearchParamResolver) (url.Values, error) {
	if patientID == "" {
		return params, nil
	}
	out := CloneURLValues(params)
	if resourceType == "Patient" {
		out.Set("_id", patientID)
		return out, nil
	}
	if resolver == nil {
		return nil, fmt.Errorf("%w for resource type %s", ErrNoPatientSearchScope, resourceType)
	}
	param, ok := resolver.PatientSearchParameterCode(resourceType)
	if !ok || param == "" {
		return nil, fmt.Errorf("%w for resource type %s", ErrNoPatientSearchScope, resourceType)
	}
	out.Set(param, "Patient/"+patientID)
	return out, nil
}

// ResourcePatientResolver extracts the patient id linked to one FHIR resource.
type ResourcePatientResolver interface {
	PatientIDForResource(ctx context.Context, resourceType string, resource *types.ResourceEnvelope) (patientID string, ok bool, err error)
}

// CheckResourcePatientScope enforces TenantContext.PatientScope against a loaded
// resource. Non-patient resources without a resolvable patient link are allowed.
func CheckResourcePatientScope(tenant TenantContext, resourceType, resourceID, patientID string, hasPatient bool) error {
	if tenant.PatientScope == "" {
		return nil
	}
	scope := tenant.PatientScope
	if resourceType == "Patient" {
		id := resourceID
		if id == "" {
			id = patientID
		}
		if id != scope {
			return fmt.Errorf("%w: principal scoped to patient %q cannot access %q", ErrDenied, scope, id)
		}
		return nil
	}
	if !hasPatient {
		return nil
	}
	if patientID != scope {
		return fmt.Errorf("%w: principal scoped to patient %q cannot access resource for patient %q", ErrDenied, scope, patientID)
	}
	return nil
}

// CheckEnvelopePatientScope enforces patient scope for one loaded resource envelope.
func CheckEnvelopePatientScope(ctx context.Context, tenant TenantContext, resolver ResourcePatientResolver, resource *types.ResourceEnvelope) error {
	if tenant.PatientScope == "" {
		return nil
	}
	if resource == nil {
		return nil
	}
	patientID, hasPatient, err := resolver.PatientIDForResource(ctx, resource.ResourceType, resource)
	if err != nil {
		return err
	}
	return CheckResourcePatientScope(tenant, resource.ResourceType, resource.ID, patientID, hasPatient)
}

// OverlayPatientID is the patient-compartment overlay target: Patient.id, else
// the compartment patient resolved from the resource (Observation.subject,
// Appointment.participant.actor, …). Empty compartment patient is unrestricted
// for non-Patient types.
func OverlayPatientID(resourceType, resourceID, compartmentPatient string) string {
	if strings.EqualFold(resourceType, "Patient") {
		return resourceID
	}
	return compartmentPatient
}

// CompartmentPatientFromJSON reads the Patient.id linked on a FHIR resource
// body. Observation uses subject.reference; Appointment uses
// participant.actor.reference. Other types use subject.reference when present.
func CompartmentPatientFromJSON(resourceType string, raw []byte) string {
	if len(raw) == 0 || strings.EqualFold(resourceType, "Patient") {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if strings.EqualFold(resourceType, "Appointment") {
		return patientIDFromAppointmentParticipants(obj["participant"])
	}
	if id := patientIDFromReferenceNode(obj["subject"]); id != "" {
		return id
	}
	return ""
}

func patientIDFromAppointmentParticipants(node any) string {
	switch list := node.(type) {
	case []any:
		for _, item := range list {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if id := patientIDFromReferenceNode(obj["actor"]); id != "" {
				return id
			}
		}
	case map[string]any:
		return patientIDFromReferenceNode(list["actor"])
	}
	return ""
}

func patientIDFromReferenceNode(node any) string {
	switch v := node.(type) {
	case string:
		return patientIDFromReferenceString(v)
	case map[string]any:
		if ref, ok := v["reference"].(string); ok {
			return patientIDFromReferenceString(ref)
		}
	}
	return ""
}

func patientIDFromReferenceString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const prefix = "Patient/"
	if strings.HasPrefix(raw, prefix) {
		return strings.TrimSpace(raw[len(prefix):])
	}
	return ""
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
