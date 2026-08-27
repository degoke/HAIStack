package sdc

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ValidationError carries a complete SDC OperationOutcome without collapsing diagnostics.
// It implements error while preserving every issue for structured callers.
type ValidationError struct {
	Outcome Outcome
	cause   error
}

func (e ValidationError) Error() string {
	return OutcomeSummary(e.Outcome)
}

func (e ValidationError) Unwrap() error {
	return e.cause
}

// OperationOutcome returns the FHIR OperationOutcome representation of the validation failure.
func (e ValidationError) OperationOutcome() types.OperationOutcome {
	return ToOperationOutcome(e.Outcome)
}

// OutcomeFromError returns the SDC Outcome when err is or wraps ValidationError.
func OutcomeFromError(err error) (Outcome, bool) {
	var ve ValidationError
	if errors.As(err, &ve) {
		return ve.Outcome, true
	}
	return Outcome{}, false
}

// HasErrors reports whether outcome contains error or fatal issues.
func HasErrors(o Outcome) bool {
	for _, issue := range o.Issue {
		if issue.Severity == "error" || issue.Severity == "fatal" {
			return true
		}
	}
	return false
}

// OutcomeSummary joins all error and fatal diagnostics into a single human-readable summary.
func OutcomeSummary(o Outcome) string {
	parts := make([]string, 0, len(o.Issue))
	for _, issue := range o.Issue {
		if issue.Severity == "error" || issue.Severity == "fatal" {
			if msg := strings.TrimSpace(issue.Diagnostics); msg != "" {
				parts = append(parts, msg)
			}
		}
	}
	if len(parts) == 0 {
		return "sdc operation failed"
	}
	return strings.Join(parts, "; ")
}

// ErrFromOutcome returns ValidationError when outcome contains error issues.
func ErrFromOutcome(o Outcome) error {
	if HasErrors(o) {
		return ValidationError{Outcome: o}
	}
	return nil
}

// NewValidationError wraps outcome and an optional underlying cause.
func NewValidationError(o Outcome, cause error) ValidationError {
	return ValidationError{Outcome: o, cause: cause}
}

// ToOperationOutcome maps an SDC Outcome into a FHIR OperationOutcome.
// Field paths are preserved in Expression for renderer and API consumers.
func ToOperationOutcome(o Outcome) types.OperationOutcome {
	if o.ResourceType == "" {
		o.ResourceType = "OperationOutcome"
	}
	issues := make([]types.OperationIssue, 0, len(o.Issue))
	for _, issue := range o.Issue {
		expression := append([]string(nil), issue.Expression...)
		if len(expression) == 0 && issue.FieldPath != "" {
			expression = []string{issue.FieldPath}
		}
		issues = append(issues, types.OperationIssue{
			Severity:    issue.Severity,
			Code:        issue.Code,
			Diagnostics: issue.Diagnostics,
			Expression:  expression,
		})
	}
	return types.OperationOutcome{
		ResourceType: o.ResourceType,
		Issue:        issues,
	}
}

// MarshalOutcome returns canonical JSON for an SDC Outcome (issues sorted for stability).
func MarshalOutcome(o Outcome) ([]byte, error) {
	return CanonicalOutcomeJSON(o)
}

// CanonicalOutcomeJSON returns stable JSON for an SDC Outcome.
func CanonicalOutcomeJSON(o Outcome) ([]byte, error) {
	normalized := Outcome{
		ResourceType: "OperationOutcome",
		Issue:        append([]Issue(nil), o.Issue...),
	}
	sort.SliceStable(normalized.Issue, func(i, j int) bool {
		return issueSortKey(normalized.Issue[i]) < issueSortKey(normalized.Issue[j])
	})
	return json.Marshal(normalized)
}

func issueSortKey(issue Issue) string {
	data, err := json.Marshal(issue)
	if err != nil {
		return issue.Code + issue.Diagnostics
	}
	return string(data)
}
