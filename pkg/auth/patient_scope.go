package auth

import (
	"fmt"
	"net/url"
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
// search parameters. Patient searches are narrowed to _id. Other resource types
// use the resolved relationship parameter (for example subject=Patient/{id}).
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
