package search

import "errors"

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
)
