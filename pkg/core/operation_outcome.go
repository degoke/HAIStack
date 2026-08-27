package core

import (
	"errors"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// OperationOutcomeFromError maps a haistack-core error to a FHIR OperationOutcome.
func OperationOutcomeFromError(err error) *types.OperationOutcome {
	if err == nil {
		return nil
	}

	if outcome := operationOutcomeFromCarrier(err); outcome != nil {
		return outcome
	}

	if outcome := operationOutcomeFromValidation(err); outcome != nil {
		return outcome
	}

	kind := KindOf(err)
	severity := "error"
	code := "exception"
	diagnostics := err.Error()

	switch kind {
	case ErrorKindInvalid:
		code = "invalid"
	case ErrorKindConflict:
		code = "conflict"
	case ErrorKindNotFound:
		code = "not-found"
	case ErrorKindNotSupported:
		code = "not-supported"
	case ErrorKindPrecondition:
		code = "processing"
	case ErrorKindException:
		code = "exception"
	}

	issue := types.OperationIssue{
		Severity:    severity,
		Code:        code,
		Diagnostics: diagnostics,
	}

	var svcErr *ServiceError
	if errors.As(err, &svcErr) && len(svcErr.Expression) > 0 {
		issue.Expression = append([]string(nil), svcErr.Expression...)
	}

	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue:        []types.OperationIssue{issue},
	}
}

func operationOutcomeFromValidation(err error) *types.OperationOutcome {
	if issues, ok := validate.IssuesFromError(err); ok {
		return validate.ToOperationOutcome(&validate.ValidationResult{Valid: false, Issues: issues})
	}
	var svcErr *ServiceError
	if errors.As(err, &svcErr) && svcErr.Cause != nil {
		if issues, ok := validate.IssuesFromError(svcErr.Cause); ok {
			return validate.ToOperationOutcome(&validate.ValidationResult{Valid: false, Issues: issues})
		}
	}
	return nil
}

type operationOutcomeCarrier interface {
	OperationOutcome() types.OperationOutcome
}

func isInvalidOutcomeError(err error) bool {
	var carrier operationOutcomeCarrier
	return errors.As(err, &carrier)
}

func operationOutcomeFromCarrier(err error) *types.OperationOutcome {
	var carrier operationOutcomeCarrier
	if errors.As(err, &carrier) {
		outcome := carrier.OperationOutcome()
		return &outcome
	}
	var svcErr *ServiceError
	if errors.As(err, &svcErr) && svcErr.Cause != nil {
		if errors.As(svcErr.Cause, &carrier) {
			outcome := carrier.OperationOutcome()
			return &outcome
		}
	}
	return nil
}
