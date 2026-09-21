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
