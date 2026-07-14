package smart

import "strings"

// LaunchContext holds SMART launch patient/user/encounter context used to feed
// haistack-auth patient-scope and principal decisions. It does not orchestrate
// EHR or standalone launch flows.
type LaunchContext struct {
	PatientID   string            `json:"patientId,omitempty"`
	EncounterID string            `json:"encounterId,omitempty"`
	UserID      string            `json:"userId,omitempty"`
	TenantHint  string            `json:"tenantHint,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// LaunchContextInput is the application-supplied input for BuildLaunchContext.
type LaunchContextInput struct {
	PatientID   string
	EncounterID string
	UserID      string
	TenantHint  string
	// Scopes optionally contribute launch markers into Metadata.
	Scopes ScopeSet
	// Metadata is copied into the result; known keys are not overwritten by
	// structured fields unless empty.
	Metadata map[string]string
	// Claims optionally supply patient/encounter/fhirUser when structured
	// fields are empty.
	Claims *TokenClaims
}

// BuildLaunchContext constructs a LaunchContext from structured fields, token
// claims, and scope markers. Empty inputs yield an empty context (not an error).
func BuildLaunchContext(in LaunchContextInput) LaunchContext {
	out := LaunchContext{
		PatientID:   strings.TrimSpace(in.PatientID),
		EncounterID: strings.TrimSpace(in.EncounterID),
		UserID:      strings.TrimSpace(in.UserID),
		TenantHint:  strings.TrimSpace(in.TenantHint),
	}
	if in.Metadata != nil {
		out.Metadata = cloneStringMap(in.Metadata)
	}
	if in.Claims != nil {
		if out.PatientID == "" {
			out.PatientID = strings.TrimSpace(in.Claims.Patient)
		}
		if out.EncounterID == "" {
			out.EncounterID = strings.TrimSpace(in.Claims.Encounter)
		}
		if out.UserID == "" {
			out.UserID = strings.TrimSpace(firstNonEmpty(in.Claims.FHIRUser, in.Claims.Subject))
		}
		if out.TenantHint == "" {
			out.TenantHint = strings.TrimSpace(in.Claims.TenantHint)
		}
		for k, v := range in.Claims.LaunchExtensions {
			if out.Metadata == nil {
				out.Metadata = make(map[string]string)
			}
			if _, exists := out.Metadata[k]; !exists {
				out.Metadata[k] = v
			}
		}
	}
	for _, sc := range in.Scopes.LaunchScopes() {
		if out.Metadata == nil {
			out.Metadata = make(map[string]string)
		}
		key := "launch"
		if sc.LaunchType != "" {
			key = "launch/" + sc.LaunchType
		}
		out.Metadata[key] = "true"
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
