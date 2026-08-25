package auth_test

import (
	"net/url"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/auth"
)

func TestApplyPatientSearchScopeToParams_PatientResource(t *testing.T) {
	params := url.Values{"name": {"Doe"}}
	out, err := auth.ApplyPatientSearchScopeToParams(params, "Patient", "pat-1", nil)
	if err != nil {
		t.Fatalf("ApplyPatientSearchScopeToParams: %v", err)
	}
	if got := out.Get("_id"); got != "pat-1" {
		t.Fatalf("_id = %q, want pat-1", got)
	}
	if got := out.Get("name"); got != "Doe" {
		t.Fatalf("name = %q, want Doe", got)
	}
}

func TestApplyPatientSearchScopeToParams_ObservationSubject(t *testing.T) {
	resolver := auth.MapPatientSearchParamResolver{"Observation": "subject"}
	params := url.Values{"code": {"8867-4"}}
	out, err := auth.ApplyPatientSearchScopeToParams(params, "Observation", "pat-1", resolver)
	if err != nil {
		t.Fatalf("ApplyPatientSearchScopeToParams: %v", err)
	}
	if got := out.Get("subject"); got != "Patient/pat-1" {
		t.Fatalf("subject = %q, want Patient/pat-1", got)
	}
}

func TestApplyPatientSearchScopeToParams_UnknownResourceType(t *testing.T) {
	resolver := auth.MapPatientSearchParamResolver{"Observation": "subject"}
	_, err := auth.ApplyPatientSearchScopeToParams(url.Values{}, "Practitioner", "pat-1", resolver)
	if err == nil {
		t.Fatal("expected error for unmapped resource type")
	}
}

func TestApplyPatientSearchScopeToParams_EmptyPatientID(t *testing.T) {
	params := url.Values{"name": {"Doe"}}
	out, err := auth.ApplyPatientSearchScopeToParams(params, "Patient", "", nil)
	if err != nil {
		t.Fatalf("ApplyPatientSearchScopeToParams: %v", err)
	}
	if out.Get("_id") != "" {
		t.Fatalf("_id = %q, want empty", out.Get("_id"))
	}
}

func TestOverridePatientSearchParamResolver(t *testing.T) {
	resolver := auth.OverridePatientSearchParamResolver{
		Base:      auth.MapPatientSearchParamResolver{"Observation": "subject"},
		Overrides: auth.MapPatientSearchParamResolver{"Observation": "patient"},
	}
	code, ok := resolver.PatientSearchParameterCode("Observation")
	if !ok || code != "patient" {
		t.Fatalf("PatientSearchParameterCode = (%q, %v), want (patient, true)", code, ok)
	}
	code, ok = resolver.PatientSearchParameterCode("Appointment")
	if ok {
		t.Fatalf("Appointment override = (%q, %v), want false", code, ok)
	}
}
