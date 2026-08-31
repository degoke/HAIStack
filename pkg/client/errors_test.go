package client

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestOutcomeDiagnosticsSummaryJoinsAllIssues(t *testing.T) {
	outcome := &types.OperationOutcome{
		Issue: []types.OperationIssue{
			{Diagnostics: "first"},
			{Diagnostics: "second"},
		},
	}
	got := outcomeDiagnosticsSummary(outcome)
	want := "first; second"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestParseErrorMessageUsesAllIssueDiagnostics(t *testing.T) {
	body := []byte(`{
		"resourceType":"OperationOutcome",
		"issue":[
			{"severity":"error","code":"required","diagnostics":"first"},
			{"severity":"error","code":"type","diagnostics":"second"}
		]
	}`)
	err := parseError(400, body, false)
	if err.Message != "first; second" {
		t.Fatalf("message = %q", err.Message)
	}
	if len(err.Outcome.Issue) != 2 {
		t.Fatalf("outcome issues = %d", len(err.Outcome.Issue))
	}
}
