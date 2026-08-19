package http

import (
	"errors"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func writeError(w http.ResponseWriter, err error) {
	status, outcome := mapError(err)
	writeOperationOutcome(w, status, outcome)
}

func mapError(err error) (int, *types.OperationOutcome) {
	if err == nil {
		return http.StatusInternalServerError, core.OperationOutcomeFromError(errors.New("unknown error"))
	}

	if errors.Is(err, auth.ErrDenied) {
		return http.StatusForbidden, deniedOutcome(err.Error())
	}
	if errors.Is(err, auth.ErrNoPatientSearchScope) {
		return http.StatusForbidden, deniedOutcome(err.Error())
	}
	if errors.Is(err, errUnauthenticated) {
		return http.StatusUnauthorized, unauthorizedOutcome(err.Error())
	}
	var rateLimited *rateLimitError
	if errors.As(err, &rateLimited) {
		return http.StatusTooManyRequests, &types.OperationOutcome{
			ResourceType: "OperationOutcome",
			Issue:        []types.OperationIssue{{Severity: "error", Code: "throttled", Diagnostics: rateLimited.Error()}},
		}
	}
	var methodErr *methodNotAllowedError
	if errors.As(err, &methodErr) {
		return http.StatusMethodNotAllowed, &types.OperationOutcome{
			ResourceType: "OperationOutcome",
			Issue:        []types.OperationIssue{{Severity: "error", Code: "not-supported", Diagnostics: methodErr.Error()}},
		}
	}
	var notAcceptable *notAcceptableError
	if errors.As(err, &notAcceptable) {
		return http.StatusNotAcceptable, &types.OperationOutcome{
			ResourceType: "OperationOutcome",
			Issue:        []types.OperationIssue{{Severity: "error", Code: "not-supported", Diagnostics: notAcceptable.Error()}},
		}
	}

	switch {
	case errors.Is(err, search.ErrInvalidQuery),
		errors.Is(err, search.ErrUnknownParam),
		errors.Is(err, search.ErrUnsupportedParam),
		errors.Is(err, search.ErrUnsupportedFeature):
		return http.StatusBadRequest, searchOutcome(err)
	case errors.Is(err, search.ErrResourceTypeDisabled):
		return http.StatusBadRequest, searchOutcome(err)
	}

	kind := core.KindOf(err)
	switch kind {
	case core.ErrorKindInvalid:
		return http.StatusBadRequest, core.OperationOutcomeFromError(err)
	case core.ErrorKindNotFound:
		return http.StatusNotFound, core.OperationOutcomeFromError(err)
	case core.ErrorKindConflict:
		return http.StatusConflict, core.OperationOutcomeFromError(err)
	case core.ErrorKindNotSupported:
		return http.StatusBadRequest, core.OperationOutcomeFromError(err)
	case core.ErrorKindPrecondition:
		return http.StatusPreconditionFailed, core.OperationOutcomeFromError(err)
	default:
		var notImpl *notImplementedError
		if errors.As(err, &notImpl) {
			return http.StatusNotImplemented, &types.OperationOutcome{
				ResourceType: "OperationOutcome",
				Issue: []types.OperationIssue{{
					Severity:    "error",
					Code:        "not-supported",
					Diagnostics: notImpl.Error(),
				}},
			}
		}
		var precond *preconditionError
		if errors.As(err, &precond) {
			return http.StatusPreconditionFailed, &types.OperationOutcome{
				ResourceType: "OperationOutcome",
				Issue: []types.OperationIssue{{
					Severity:    "error",
					Code:        "processing",
					Diagnostics: precond.Error(),
				}},
			}
		}
		return http.StatusInternalServerError, core.OperationOutcomeFromError(err)
	}
}

func searchOutcome(err error) *types.OperationOutcome {
	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "invalid",
			Diagnostics: err.Error(),
		}},
	}
}

func deniedOutcome(message string) *types.OperationOutcome {
	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "forbidden",
			Diagnostics: message,
		}},
	}
}

func unauthorizedOutcome(message string) *types.OperationOutcome {
	return &types.OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue: []types.OperationIssue{{
			Severity:    "error",
			Code:        "security",
			Diagnostics: message,
		}},
	}
}
