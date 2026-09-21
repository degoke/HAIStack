package policysemantics_test

import (
	"context"
	"testing"

	policysemantics "github.com/degoke/health-ai-stack/research/policy-semantics"
)

func TestCatalogSemantics(t *testing.T) {
	scenarios, err := policysemantics.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) < 10 {
		t.Fatalf("catalogue size = %d, want >= 10", len(scenarios))
	}
	report, err := policysemantics.RunCatalog(context.Background(), scenarios)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed > 0 {
		for _, r := range report.Results {
			if !r.Pass {
				t.Errorf("%s: allowed=%v want %v (%s)", r.ID, r.Allowed, r.ExpectAllow, r.Reason)
			}
		}
	}
	if report.Passed < 10 {
		t.Fatalf("passed = %d, want >= 10", report.Passed)
	}
}

func TestSMARTDenyIsNotOverwrittenByConsentDeny(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:            "smart-before-consent",
		Principal:     "clinician",
		Policy:        "narrow-observation",
		Scopes:        "patient/Condition.read",
		LaunchPatient: "pat-1",
		Action:        "read",
		ResourceType:  "Observation",
		ResourceID:    "obs-1",
		Consent: &policysemantics.ConsentState{
			Status:        "active",
			PatientID:     "pat-1",
			ProvisionType: "deny",
			ResourceTypes: []string{"Observation"},
			Actions:       []string{"read"},
		},
		ExpectAllow: false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	got := report.Results[0].Reason
	if got != "SMART scope does not grant Observation.read" {
		t.Fatalf("SMART must fail before consent overlay, got %q", got)
	}
}

func TestConsentDenyRunsBeforePolicy(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:           "consent-before-policy",
		Principal:    "clinician",
		Policy:       "narrow-observation",
		Action:       "read",
		ResourceType: "Appointment",
		ResourceID:   "a1",
		Consent: &policysemantics.ConsentState{
			Status:        "active",
			ProvisionType: "deny",
			ResourceTypes: []string{"Appointment"},
			Actions:       []string{"read"},
		},
		ExpectAllow: false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Results[0].Reason != "consent deny provision" {
		t.Fatalf("consent must deny before policy, got %q", report.Results[0].Reason)
	}
}

func TestIdentityDenyIsNotOverwrittenByConsentDeny(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:           "identity-before-consent",
		Principal:    "clinician",
		Policy:       "base",
		Tenant:       "tenant-b",
		Action:       "read",
		ResourceType: "Observation",
		ResourceID:   "obs-1",
		Consent: &policysemantics.ConsentState{
			Status:        "active",
			ProvisionType: "deny",
			ResourceTypes: []string{"Observation"},
			Actions:       []string{"read"},
		},
		ExpectAllow: false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	want := `principal "user-clinician" is not bound to tenant "tenant-b"`
	if report.Results[0].Reason != want {
		t.Fatalf("identity must fail before consent, got %q", report.Results[0].Reason)
	}
}

func TestPatientOverlayDenyIsNotOverwrittenByConsentDeny(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:           "patient-before-consent",
		Principal:    "clinician",
		Policy:       "base",
		PatientScope: "pat-1",
		Action:       "read",
		ResourceType: "Patient",
		ResourceID:   "pat-2",
		Consent: &policysemantics.ConsentState{
			Status:        "active",
			PatientID:     "pat-2",
			ProvisionType: "deny",
			ResourceTypes: []string{"Patient"},
			Actions:       []string{"read"},
		},
		ExpectAllow: false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	want := `principal scoped to patient "pat-1" cannot access "pat-2"`
	if report.Results[0].Reason != want {
		t.Fatalf("patient overlay must fail before consent, got %q", report.Results[0].Reason)
	}
}

func TestPatientOverlayObservationDenyIsNotOverwrittenByConsentDeny(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:                 "obs-compartment-before-consent",
		Principal:          "clinician",
		Policy:             "narrow-observation",
		PatientScope:       "pat-1",
		CompartmentPatient: "pat-2",
		Action:             "read",
		ResourceType:       "Observation",
		ResourceID:         "obs-1",
		Consent: &policysemantics.ConsentState{
			Status:        "active",
			PatientID:     "pat-2",
			ProvisionType: "deny",
			ResourceTypes: []string{"Observation"},
			Actions:       []string{"read"},
		},
		ExpectAllow: false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	want := `principal scoped to patient "pat-1" cannot access "pat-2"`
	if report.Results[0].Reason != want {
		t.Fatalf("Observation compartment overlay must fail before consent, got %q", report.Results[0].Reason)
	}
}

func TestOverlayPatientID(t *testing.T) {
	if got := policysemantics.OverlayPatientID(policysemantics.Scenario{
		ResourceType: "Patient", ResourceID: "pat-1", CompartmentPatient: "pat-9", LaunchPatient: "pat-8",
	}); got != "pat-1" {
		t.Fatalf("Patient.id = %q", got)
	}
	if got := policysemantics.OverlayPatientID(policysemantics.Scenario{
		ResourceType: "Observation", ResourceID: "obs-1", CompartmentPatient: "pat-2", LaunchPatient: "pat-1",
	}); got != "pat-2" {
		t.Fatalf("compartment = %q", got)
	}
	if got := policysemantics.OverlayPatientID(policysemantics.Scenario{
		ResourceType: "Observation", ResourceID: "obs-1", LaunchPatient: "pat-1",
	}); got != "pat-1" {
		t.Fatalf("launch fallback = %q", got)
	}
}

func TestSMARTWriteDeny(t *testing.T) {
	sc := policysemantics.Scenario{
		ID:            "smart-write-deny",
		Principal:     "clinician",
		Policy:        "base",
		Scopes:        "patient/Appointment.read",
		LaunchPatient: "pat-1",
		Action:        "write",
		ResourceType:  "Appointment",
		ResourceID:    "a1",
		ExpectAllow:   false,
	}
	report, err := policysemantics.RunCatalog(context.Background(), []policysemantics.Scenario{sc})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	got := report.Results[0].Reason
	if got != "SMART scope does not grant Appointment.write" {
		t.Fatalf("SMART write must be checked, got %q", got)
	}
}
