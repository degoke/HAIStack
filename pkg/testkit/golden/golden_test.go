package golden_test

import (
	"encoding/json"
	"errors"
	"fmt"
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

func TestCanonicalOutcomeJSONSortsIssues(t *testing.T) {
	first, err := golden.CanonicalOutcomeJSON(types.OperationOutcome{Issue: []types.OperationIssue{
		{Code: "z"}, {Code: "a"},
	}})
	if err != nil {
		t.Fatalf("canonical first: %v", err)
	}
	second, err := golden.CanonicalOutcomeJSON(types.OperationOutcome{Issue: []types.OperationIssue{
		{Code: "a"}, {Code: "z"},
	}})
	if err != nil {
		t.Fatalf("canonical second: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical output differs by issue order: %s vs %s", first, second)
	}
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

type wrappedOutcomeError struct{}

func (wrappedOutcomeError) Error() string { return "wrapped outcome" }

func (wrappedOutcomeError) OperationOutcome() types.OperationOutcome {
	return types.OperationOutcome{Issue: []types.OperationIssue{{Code: "invalid"}}}
}

func TestOutcomeFromErrorHandlesWrappedAndCoreErrors(t *testing.T) {
	got, ok := golden.OutcomeFromError(fmt.Errorf("outer: %w", wrappedOutcomeError{}))
	if !ok || got.Issue[0].Code != "invalid" {
		t.Fatalf("wrapped outcome = %+v, %v", got, ok)
	}
	got, ok = golden.OutcomeFromError(errors.New("ordinary error"))
	if !ok || len(got.Issue) != 1 || got.Issue[0].Code != "exception" {
		t.Fatalf("ordinary outcome = %+v, %v", got, ok)
	}
}

func TestFormatMismatchEmptyWantIsReadable(t *testing.T) {
	diff := golden.FormatMismatch([]byte(`{"code":"x"}`), nil)
	if diff != "got:\n{\n  \"code\": \"x\"\n}\nwant:\n" {
		t.Fatalf("diff = %q", diff)
	}
}
