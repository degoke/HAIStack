package cql

import (
	"errors"
	"fmt"
)

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

var (
	// ErrMissingContext is returned when a Patient-context expression is
	// evaluated without a subject Patient.
	ErrMissingContext = errors.New("CQL Patient context is missing")

	// ErrLibraryNotFound is returned when a referenced CQL library cannot be loaded.
	ErrLibraryNotFound = errors.New("CQL library was not found")

	// ErrExpressionNotFound is returned when a named define is missing from loaded libraries.
	ErrExpressionNotFound = errors.New("CQL expression was not found in the loaded libraries")

	// ErrEngineUnavailable is returned when evaluation is attempted without an engine.
	ErrEngineUnavailable = errors.New("CQL engine is unavailable")

	// ErrUnsupported is returned for CQL features that are not implemented,
	// including ELM-only libraries.
	ErrUnsupported = errors.New("CQL feature is not supported")

	// ErrEmptyExpression is returned when the expression text is empty.
	ErrEmptyExpression = errors.New("CQL expression is empty")

	// ErrMeasure is returned when a FHIR Measure cannot be evaluated.
	ErrMeasure = errors.New("CQL measure evaluation failed")
)
