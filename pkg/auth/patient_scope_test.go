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
	params := url.Values{"code": {"8867-4"}}
	out, err := auth.ApplyPatientSearchScopeToParams(params, "Observation", "pat-1", nil)
	if err != nil {
		t.Fatalf("ApplyPatientSearchScopeToParams: %v", err)
	}
	if got := out.Get("subject"); got != "Patient/pat-1" {
		t.Fatalf("subject = %q, want Patient/pat-1", got)
	}
}

func TestApplyPatientSearchScopeToParams_UnknownResourceType(t *testing.T) {
	_, err := auth.ApplyPatientSearchScopeToParams(url.Values{}, "Practitioner", "pat-1", nil)
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

func TestMergePatientSearchParams(t *testing.T) {
	merged := auth.MergePatientSearchParams(map[string]string{
		"Observation": "patient",
		"CustomType":  "subject",
	})
	if merged["Observation"] != "patient" {
		t.Fatalf("Observation override = %q", merged["Observation"])
	}
	if merged["CustomType"] != "subject" {
		t.Fatalf("CustomType = %q", merged["CustomType"])
	}
	if merged["Appointment"] != "patient" {
		t.Fatalf("Appointment default = %q", merged["Appointment"])
	}
}
