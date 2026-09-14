package verily

import "errors"

var (
	// ErrShadowsBuiltin is returned when a custom function name conflicts with a built-in.
	ErrShadowsBuiltin = errors.New("fhirpath: custom function shadows a built-in name")

	// ErrNotSupported is returned for expressions requiring unsupported capabilities.
	ErrNotSupported = errors.New("fhirpath: expression requires unsupported capability")

	// ErrInvalidInput is returned when the evaluation root is not a supported type.
	ErrInvalidInput = errors.New("fhirpath: invalid evaluation input")

	// ErrUnconfiguredResolver is returned when resolve() is used without a resolver.
	ErrUnconfiguredResolver = errors.New("fhirpath: resolve() requires a configured resolver")
)
