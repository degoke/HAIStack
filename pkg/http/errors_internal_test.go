package http

import (
	"net/http"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestMapErrorValidationErrorReturns400WithAllIssues(t *testing.T) {
	err := sdc.ValidationError{
		Outcome: sdc.Outcome{
			Issue: []sdc.Issue{
				{Severity: "error", Code: "required", Diagnostics: "first"},
				{Severity: "error", Code: "type", Diagnostics: "second"},
			},
		},
	}
	status, outcome := mapError(err)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if outcome == nil || len(outcome.Issue) != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestMapErrorQuestionnaireNotFoundReturns404(t *testing.T) {
	err := types.NewQuestionnaireNotFoundError("http://example/missing", types.ErrQuestionnaireNotFound)
	status, outcome := mapError(err)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if outcome == nil || len(outcome.Issue) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
}
