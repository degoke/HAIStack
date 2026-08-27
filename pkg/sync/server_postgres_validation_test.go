package sync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type stubValidator struct {
	err error
}

func (s stubValidator) ValidateResource(context.Context, *types.ResourceEnvelope) error {
	return s.err
}

func TestBuildHubWriteReturnsStructuredValidationOutcome(t *testing.T) {
	validator := stubValidator{err: validate.FailedError{Issues: []validate.ValidationIssue{
		{Severity: "error", Code: "required", Diagnostics: "first issue", Expression: []string{"item[a]"}},
		{Severity: "error", Code: "type", Diagnostics: "second issue", Expression: []string{"item[b]"}},
	}}}

	_, reject, ok := buildHubWrite(context.Background(), LocalEvent{
		EventID:      "event-3",
		ResourceType: "Patient",
		ResourceID:   "p1",
		Operation:    EventTypeResourceCreated,
		ResourceAfter: &types.ResourceEnvelope{
			ResourceType: "Patient",
			ID:           "p1",
			JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
		},
	}, time.Now().UTC(), validator)
	if ok {
		t.Fatal("expected rejection")
	}
	if reject.Outcome == nil || len(reject.Outcome.Issue) != 2 {
		t.Fatalf("outcome = %+v", reject.Outcome)
	}
	if !strings.Contains(reject.Reason, "first issue") || !strings.Contains(reject.Reason, "second issue") {
		t.Fatalf("reason = %q", reject.Reason)
	}
}
