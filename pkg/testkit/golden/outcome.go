package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// CanonicalOutcomeJSON returns stable JSON for an OperationOutcome (sorted issues).
func CanonicalOutcomeJSON(outcome types.OperationOutcome) ([]byte, error) {
	normalized := types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue:        append([]types.OperationIssue(nil), outcome.Issue...),
	}
	if normalized.ResourceType == "" {
		normalized.ResourceType = "OperationOutcome"
	}
	return json.Marshal(normalized)
}

// AssertOutcomeEqual compares two OperationOutcomes ignoring JSON formatting differences.
func AssertOutcomeEqual(t *testing.T, got, want types.OperationOutcome) {
	t.Helper()
	gotJSON, err := CanonicalOutcomeJSON(got)
	if err != nil {
		t.Fatalf("canonical got outcome: %v", err)
	}
	wantJSON, err := CanonicalOutcomeJSON(want)
	if err != nil {
		t.Fatalf("canonical want outcome: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("OperationOutcome mismatch:\ngot:  %s\nwant: %s", prettyJSON(gotJSON), prettyJSON(wantJSON))
	}
}

// AssertOutcomeMatchesGolden compares an outcome to an inline golden JSON payload.
func AssertOutcomeMatchesGolden(t *testing.T, got types.OperationOutcome, goldenJSON string) {
	t.Helper()
	var want types.OperationOutcome
	if err := json.Unmarshal([]byte(goldenJSON), &want); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	AssertOutcomeEqual(t, got, want)
}

// DecodeOutcome unmarshals OperationOutcome JSON from a response body.
func DecodeOutcome(t *testing.T, body []byte) types.OperationOutcome {
	t.Helper()
	var outcome types.OperationOutcome
	if err := json.Unmarshal(body, &outcome); err != nil {
		t.Fatalf("golden.DecodeOutcome: %v", err)
	}
	return outcome
}

// AssertOutcomeCode asserts the first issue code matches want.
func AssertOutcomeCode(t *testing.T, outcome types.OperationOutcome, want string) {
	t.Helper()
	if len(outcome.Issue) == 0 {
		t.Fatalf("outcome has no issues, want code %q", want)
	}
	if outcome.Issue[0].Code != want {
		t.Fatalf("issue code = %q, want %q", outcome.Issue[0].Code, want)
	}
}

// AssertOutcomeIssueCount asserts the number of issues.
func AssertOutcomeIssueCount(t *testing.T, outcome types.OperationOutcome, want int) {
	t.Helper()
	if len(outcome.Issue) != want {
		t.Fatalf("issue count = %d, want %d", len(outcome.Issue), want)
	}
}

// OutcomeFromError renders an OperationOutcome from a Go error value when possible.
func OutcomeFromError(err error) (types.OperationOutcome, bool) {
	type outcomeCarrier interface {
		OperationOutcome() types.OperationOutcome
	}
	if c, ok := err.(outcomeCarrier); ok {
		return c.OperationOutcome(), true
	}
	return types.OperationOutcome{}, false
}

// FormatMismatch returns a readable diff string for two outcome JSON blobs.
func FormatMismatch(got, want []byte) string {
	return fmt.Sprintf("got:\n%s\nwant:\n%s", prettyJSON(got), prettyJSON(want))
}

func prettyJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return strings.TrimSpace(string(data))
	}
	return buf.String()
}
