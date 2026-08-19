package search

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedParam indicates a search parameter is not supported in the MVP.
	ErrUnsupportedParam = errors.New("search: unsupported search parameter")
	// ErrUnsupportedFeature indicates a FHIR search feature is not implemented.
	ErrUnsupportedFeature = errors.New("search: unsupported search feature")
	// ErrUnknownParam indicates a parameter is not defined for the resource type.
	ErrUnknownParam = errors.New("search: unknown search parameter")
	// ErrInvalidQuery indicates malformed search input.
	ErrInvalidQuery = errors.New("search: invalid query")
	// ErrResourceTypeDisabled indicates the resource type is not enabled in the registry.
	ErrResourceTypeDisabled = errors.New("search: resource type not enabled")
	// ErrProjectionFailed indicates response projection could not be applied safely.
	ErrProjectionFailed = errors.New("search: projection failed")
)

// UnknownParamError identifies an unknown search parameter code for a resource type.
type UnknownParamError struct {
	ResourceType string
	Code         string
}

func (e UnknownParamError) Error() string {
	if e.ResourceType != "" {
		return fmt.Sprintf("%s: %q on %s", ErrUnknownParam, e.Code, e.ResourceType)
	}
	return fmt.Sprintf("%s: %q", ErrUnknownParam, e.Code)
}

func (e UnknownParamError) Unwrap() error { return ErrUnknownParam }

// UnknownParamCode returns the unknown parameter code when err wraps UnknownParamError.
func UnknownParamCode(err error) (string, bool) {
	var u UnknownParamError
	if errors.As(err, &u) {
		return u.Code, true
	}
	return "", false
}
