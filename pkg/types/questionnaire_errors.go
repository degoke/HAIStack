package types

import (
	"errors"
	"fmt"
)

// ErrQuestionnaireNotFound indicates a QuestionnaireResponse.questionnaire reference could not be resolved.
var ErrQuestionnaireNotFound = errors.New("questionnaire not found")

// QuestionnaireReferenceError classifies questionnaire resolution failures for HTTP and core mapping.
type QuestionnaireReferenceError struct {
	Canonical  string
	Kind       string // "not-found", "resolver-required", or "resolution-failed"
	Expression []string
	Cause      error
}

func (e QuestionnaireReferenceError) Error() string {
	switch e.Kind {
	case "not-found":
		return fmt.Sprintf("questionnaire not found: %s", e.Canonical)
	case "resolver-required":
		return "questionnaire resolver is required for QuestionnaireResponse validation"
	default:
		if e.Cause != nil {
			return fmt.Sprintf("questionnaire resolution failed: %v", e.Cause)
		}
		return "questionnaire resolution failed"
	}
}

func (e QuestionnaireReferenceError) Unwrap() error {
	return e.Cause
}

func (e QuestionnaireReferenceError) Is(target error) bool {
	return target == ErrQuestionnaireNotFound && e.Kind == "not-found"
}

// NewQuestionnaireNotFoundError returns a not-found questionnaire reference error.
func NewQuestionnaireNotFoundError(canonical string, cause error) QuestionnaireReferenceError {
	return QuestionnaireReferenceError{
		Canonical:  canonical,
		Kind:       "not-found",
		Expression: []string{"QuestionnaireResponse.questionnaire"},
		Cause:      cause,
	}
}

// NewQuestionnaireResolverRequiredError returns a client error when validation cannot resolve a questionnaire.
func NewQuestionnaireResolverRequiredError() QuestionnaireReferenceError {
	return QuestionnaireReferenceError{
		Kind:       "resolver-required",
		Expression: []string{"QuestionnaireResponse.questionnaire"},
	}
}

// NewQuestionnaireResolutionFailedError returns an infrastructure resolution failure.
func NewQuestionnaireResolutionFailedError(cause error) QuestionnaireReferenceError {
	return QuestionnaireReferenceError{
		Kind:  "resolution-failed",
		Cause: cause,
	}
}
