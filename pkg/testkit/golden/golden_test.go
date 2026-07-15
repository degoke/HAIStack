package golden_test

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/golden"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestAssertOutcomeEqualIgnoresFormatting(t *testing.T) {
	got := types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "not-found",
			Diagnostics: "missing",
		}},
	}
	wantJSON := `{
		"resourceType": "OperationOutcome",
		"issue": [{
			"severity":"error",
			"code":"not-found",
			"diagnostics":"missing"
		}]
	}`
	var want types.OperationOutcome
	_ = json.Unmarshal([]byte(wantJSON), &want)
	golden.AssertOutcomeEqual(t, got, want)
}

func TestAssertOutcomeMatchesGolden(t *testing.T) {
	outcome := types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue:        []types.OperationIssue{{Code: "invalid", Severity: "error"}},
	}
	golden.AssertOutcomeMatchesGolden(t, outcome, `{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"invalid"}]}`)
}

func TestDecodeOutcomeAndAssertCode(t *testing.T) {
	body := []byte(`{"resourceType":"OperationOutcome","issue":[{"code":"processing","severity":"error"}]}`)
	outcome := golden.DecodeOutcome(t, body)
	golden.AssertOutcomeCode(t, outcome, "processing")
	golden.AssertOutcomeIssueCount(t, outcome, 1)
}

func TestMismatchDiagnostics(t *testing.T) {
	got, _ := golden.CanonicalOutcomeJSON(types.OperationOutcome{Issue: []types.OperationIssue{{Code: "a"}}})
	want, _ := golden.CanonicalOutcomeJSON(types.OperationOutcome{Issue: []types.OperationIssue{{Code: "b"}}})
	diff := golden.FormatMismatch(got, want)
	if diff == "" {
		t.Fatal("expected non-empty mismatch")
	}
}
