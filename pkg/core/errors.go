package core

import (
	"errors"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ErrorKind classifies haistack-core service errors for OperationOutcome mapping.
type ErrorKind string

const (
	// ErrorKindInvalid indicates the request or resource failed validation or syntax checks.
	ErrorKindInvalid ErrorKind = "invalid"
	// ErrorKindConflict indicates a duplicate create or version conflict.
	ErrorKindConflict ErrorKind = "conflict"
	// ErrorKindNotFound indicates the target resource does not exist.
	ErrorKindNotFound ErrorKind = "not-found"
	// ErrorKindNotSupported indicates the requested operation or bundle shape is not implemented.
	ErrorKindNotSupported ErrorKind = "not-supported"
	// ErrorKindPrecondition indicates an If-Match or other request precondition failed.
	ErrorKindPrecondition ErrorKind = "precondition-failed"
	// ErrorKindException indicates an unexpected or storage-layer failure.
	ErrorKindException ErrorKind = "exception"
)

// ServiceError is a typed haistack-core error.
type ServiceError struct {
	Kind       ErrorKind
	Message    string
	Expression []string
	Cause      error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// KindOf returns the ErrorKind for err when it is or wraps a ServiceError.
// Unrecognized errors return ErrorKindException.
func KindOf(err error) ErrorKind {
	var clientValidation types.ClientValidationOutcomeError
	if errors.As(err, &clientValidation) {
		return ErrorKindInvalid
	}
	if errors.Is(err, types.ErrQuestionnaireNotFound) {
		return ErrorKindNotFound
	}
	var qref types.QuestionnaireReferenceError
	if errors.As(err, &qref) {
		switch qref.Kind {
		case "not-found":
			return ErrorKindNotFound
		case "resolver-required":
			return ErrorKindInvalid
		case "resolution-failed":
			return ErrorKindException
		}
	}
	var svcErr *ServiceError
	if errors.As(err, &svcErr) && svcErr != nil {
		return svcErr.Kind
	}
	return ErrorKindException
}

func invalidErr(message string, cause error, expression ...string) *ServiceError {
	return &ServiceError{Kind: ErrorKindInvalid, Message: message, Cause: cause, Expression: expression}
}

func conflictErr(message string, cause error) *ServiceError {
	return &ServiceError{Kind: ErrorKindConflict, Message: message, Cause: cause}
}

func notFoundErr(message string, cause error) *ServiceError {
	return &ServiceError{Kind: ErrorKindNotFound, Message: message, Cause: cause}
}

func notSupportedErr(message string, cause error) *ServiceError {
	return &ServiceError{Kind: ErrorKindNotSupported, Message: message, Cause: cause}
}

func preconditionErr(message string, cause error) *ServiceError {
	return &ServiceError{Kind: ErrorKindPrecondition, Message: message, Cause: cause}
}

func exceptionErr(message string, cause error) *ServiceError {
	return &ServiceError{Kind: ErrorKindException, Message: message, Cause: cause}
}

// IsNotFound reports whether err is a not-found service error.
func IsNotFound(err error) bool {
	return KindOf(err) == ErrorKindNotFound
}

// IsConflict reports whether err is a conflict service error.
func IsConflict(err error) bool {
	return KindOf(err) == ErrorKindConflict
}

