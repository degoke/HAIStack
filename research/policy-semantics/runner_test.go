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
